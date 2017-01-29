require_relative 'build'
require_relative 'service'

# --------------------------------------------------------------------------
# IntegrationTest runs integration tests for APTrust and DPN code,
# as well as the APTrust Go services unit test suite. Integration
# tests run against a local Pharos server and a local DPN cluster,
# both of which are emptied and then re-seeded with essential fixture
# data before each test run.
#
# Most integration tests depend on the outcome of prior integration tests.
# For example, it's impossible to run the test that marks APTrust
# bags for DPN unless prior tests have actually loaded those bags into
# Pharos. Integration tests that require other tests to run first will
# run those test automatically. The general chain of events here, which
# mirrors the chaing of events in production, looks like this for APTrust
# Ingest:
#
# 1. The bucket reader scans receiving buckets and creates new WorkItems
#    in Pharos and then adds the ids of those WorkItems to the NSQ
#    fetch_topic.
# 2. apt_fetch reads the WorkItem ids from NSQ fetch_channel, copies tar
#    files from the receiving buckets, and validates them. If a bag is
#    valid, the WorkItem id is pushed into store_topic in NSQ. Whether
#    the bag is valid or not, apt_fetch records information about the
#    status of its work in the WorkItem record, and it stores a JSON
#    representation of the state of its work in WorkItemState.
# 3. apt_store reads WorkItem ids from the NSQ store_channel. It stores
#    GenericFiles in the APTrust preservation storage bucket (S3 Virginia)
#    and in Glacier preservation storage in Oregon. Then it pushes the
#    WorkItem id into the record_topic.
# 4. apt_record reads WorkItem ids from the NSQ record_channel. From there
#    it gets the WorkItemState (a JSON representation of the state of the
#    entire IntellectualObject and its files and events), and begins
#    recording that state in Pharos (creating an IntellectualObject record,
#    GenericFile records, and PREMIS event records).
#
# After Ingest, we can do any of the following:
#
# * Mark bags for DPN by creating a WorkItem for the bag with action='DPN'.
#   In integration tests, the test app apt_send_to_dpn marks a number of
#   ingested test bags to go to DPN. The dpn_queue cron job will create
#   NSQ entries for each of these items. (In demo and production, it's a
#   cron job. Here, it's a method.)
#
# * Mark bags to be restored by creating a WorkItem for the bag where
#   action='Restore'.
#
# * Mark IntellectualObjects and/or GenericFiles to be deleted by creating
#   a WorkItem where action='Delete'. Note that one delete WorkItem will be
#   created for the bag, and one for EACH GenericFile in the bag.
#
# For DPN Ingest, the process goes as follows. Note that, like the apt
# processes, the dpn processes always update an item's WorkItem and
# WorkItemState records in Pharos before moving on to the next item.
#
# 1. The cron job dpn_queue finds WorkItems in Pharos describing which
#    bags should be pushed to DPN. It pushes the id of each of these
#    WorkItems into NSQ's dpn_ingest topic.
# 2. dpn_package pulls items from the dpn_ingest channel, fetches all
#    of the files that make up the bag, and packs them all into a DPN
#    bag. A DPN bag is slightly different from an APTrust bag, containing
#    DPN-specific manifests, tag files, and tag manifests. The packager
#    then pushes the WorkItem id into the dpn_ingest_store topic in NSQ.
# 3. dpn_ingest_store pulls the WorkItem id from NSQ's dpn_store channel
#    and copies the entire tarred bag as a single file into our DPN preservation
#    storage area in Glacier/Virginia. dpn_ingest_store then pushes the
#    WorkItem id into the dpn_record topic.
# 4. dpn_record reads from the dpn_record channel. It creates a new DPN
#    bag record in the local DPN REST service, and it creates replication
#    requests in the local DPN REST service for two other nodes to
#    replicate the new bag. It creates symlinks to the copy of the bag
#    in our staging area, so the other nodes can copy via rsync. This
#    means a copy of the bag will sit in our staging area (local EBS
#    or EFS volume) until two other nodes have replicated it.
# 5. dpn_cleanup runs as a cron job, deleting all DPN bags from our
#    staging area that have been replicated twice. The deletion removes
#    the tar file iteself (which can be hundreds of GB in size) as well
#    as the symlinks to that bag in /home/dpn.tdr, /home/dpn.sdr, etc.
#
# For DPN replication, the process goes like this:
#
# 1. The cron job dpn_sync copies new bag records and replication requests
#    from remote nodes into our local DPN REST service.
# 2. The cron job dpn_queue queries our local DPN REST service and creates a
#    DPNWorkItem in Pharos for each new replication request where the to_node
#    is APTrust. These are requests where other nodes want to copy bags to
#    our node. dpn_queue then puts the id of each of these new DPNWorkItems
#    into NSQ's dpn_copy topic.
# 3. dpn_copy reads from the dpn_copy channel and copies bags via rsync from
#    the remote nodes. It calculates the checksum of the tag manifest of each
#    bag, and sends that checksum back to the originating node. If the
#    originating says the checksum is good, dpn_copy will perform a full
#    validation on the bag (which can take hours). If the bag is valid,
#    dpn_copy pushes its DPNWorkItem id into the dpn_replication_store queue.
# 4. dpn_replication_store stores the bag in Glacier/VA, as described above, and then
#    pushes the DPNWorkItem into the dpn_record topic of NSQ.
# 5. dpn_record tell the remote node that the bag was stored. Note that our
#    own node will not know that the bag has been stored until next time
#    dpn_sync pulls data from the remote node.
#
# These integration tests [will soon] perform all of these operations
# against locally-running services. The only outside services these integration
# tests touch are S3 and Glacier. Integration test bags are in the S3 bucket
# aptrust.receiving.test.test.edu, and if those are ever deleted, they can
# be restored from testdata/s3_bags/TestBags.zip. These tests store ingested
# and replicated bags in the APTrust and DPN preservation test buckets, which
# should be emptied periodically.
#
# --------------------------------------------------------------------------
class IntegrationTest

  def initialize(context)
	@context = context
	@build = Build.new(context)
	@service = Service.new(context)
	@results = {}
	@context.make_test_dirs
	@context.clear_logs
	@context.clear_staging
	@context.clear_restore
	@context.clear_binaries
	@context.clear_nsq_data
  end


  # apt_bucket_reader scans depositor receiving buckets on S3 for
  # new files that need to be ingested. It creates WorkItem entries
  # and NSQ tasks for each new tar file in the receiving buckets.
  # It will not create a new WorkItem + NSQ entry if a WorkItem already
  # exists for the tar file.
  def apt_bucket_reader(more_tests_follow)
	run_suite(more_tests_follow) do
	  # Build everything anew
	  @build.build(@context.apps['nsq_service'])
	  @build.build(@context.apps['apt_bucket_reader'])

	  # Start services with a little extra time for startup and shutdown
	  @service.pharos_reset_db
	  @service.pharos_load_fixtures
	  @service.pharos_start
	  @service.nsq_start
	  sleep 10
	  @service.app_start(@context.apps['apt_bucket_reader'])
	  @service.stop_everything unless more_tests_follow
	  sleep 5

	  # Run the post tests.
	  @results['apt_bucket_reader_test'] = run('apt_bucket_reader_post_test.go')
	end
  end

  # apt_ingest runs the entire APTrust ingest process, from end to end,
  # using fixtures, local services, and AWS S3/Glacier.
  def apt_ingest(more_tests_follow)

	# apt_ingest can be called from more than one method below.
	# If it has already run, it will have recorded the results
	# of apt_record_test, and we don't want to run it again.
	if !@results['apt_record_test'].nil?
	  return true
	end

	run_suite(more_tests_follow) do
	  # Rebuild binaries
	  @build.build(@context.apps['apt_volume_service'])
	  @build.build(@context.apps['apt_fetch'])
	  @build.build(@context.apps['apt_store'])
	  @build.build(@context.apps['apt_record'])

	  # Run the prerequisite process (with tests)
	  # Note that the prereq starts most of the required services.
	  apt_bucket_reader_ok = apt_bucket_reader(true)
	  if !apt_bucket_reader_ok
		puts "Skipping apt_ingest test because of prior failures."
		return false
	  end

	  # Start services required for this specific set of tests.
	  @service.app_start(@context.apps['apt_volume_service'])
	  sleep 5
	  @service.app_start(@context.apps['apt_fetch'])
	  sleep 20  # let nsq store topic fill before client connects
	  @service.app_start(@context.apps['apt_store'])
	  sleep 20  # let nsq record topic fill before client connects
	  @service.app_start(@context.apps['apt_record'])
	  sleep 50  # allow fetch/store/record time to finish
	  @service.stop_everything unless more_tests_follow
	  sleep 5

	  # Run the post tests. This is where we check to see if the
	  # ingest services (fetch, store, record) correctly performed
	  # all of the expected work.
	  @results['apt_fetch_test'] = run('apt_fetch_post_test.go')
	  @results['apt_store_test'] = run('apt_store_post_test.go')
	  @results['apt_record_test'] = run('apt_record_post_test.go')
	end
  end

  # apt_queue copies WorkItems into NSQ. For example, any oustanding
  # requests to delete files, restore files, send files to DPN, etc.,
  # that have no queued_at timestamp will be put into the appropriate
  # NSQ topic.
  def apt_queue(more_tests_follow)
	# Don't run this if it's already been run.
	if !@results['apt_queue_test'].nil?
	  return true
	end
	run_suite(more_tests_follow) do
	  @build.build(@context.apps['apt_queue'])

	  # Run the prerequisite process (with tests)
	  # Note that the prereq starts most of the required services.
	  apt_ingest_ok = apt_ingest(true)
	  if !apt_ingest_ok
		puts "Skipping apt_queue test because of prior failures."
		return false
	  end

	  # Mark some IntellectualObjects for restoration in Pharos,
	  # so that apt_restore and apt_file_delete will have something to work on.
      # Marking an item for deletion causes Pharos to initiate a
      # multi-step DB transaction. It should finish in 1/2 second or less,
      # but give it 5 seconds to be save. Without any sleep, we'll often
      # get a 'database is locked' exception in the Pharos logs, and then
      # subsequent test operations fail.
	  @results['apt_mark_for_restore'] = run('apt_mark_for_restore_test.go')
	  @results['apt_mark_for_delete'] = run('apt_mark_for_delete_test.go')
      sleep 5

	  # apt_queue is not a service. It runs to completion, then exits.
	  # For integration tests, it should take just a second or two.
	  @service.app_start(@context.apps['apt_queue'])
	  @service.stop_everything unless more_tests_follow
	  sleep 5

	  # Run the post tests.
	  @results['apt_queue_test'] = run('apt_queue_post_test.go')
	end
  end

  # apt_restore runs the APTrust bag restoration service to restore
  # a number of bags. It also runs file deletions.
  def apt_restore(more_tests_follow)
	run_suite(more_tests_follow) do
	  @build.build(@context.apps['apt_restore'])
	  @build.build(@context.apps['apt_file_delete'])

	  # Run the prerequisite process (with tests)
	  # Note that the prereq starts most of the required services,
	  # and apt_queue marks items for restore and pushes them into
	  # NSQ.
	  apt_queue_ok = apt_queue(true)
	  if !apt_queue_ok
		puts "Skipping apt_restore test because of prior failures."
		return false
	  end

	  # Start services required for this specific set of tests.
	  @service.app_start(@context.apps['apt_restore'])
	  @service.app_start(@context.apps['apt_file_delete'])
	  sleep 45

	  # Run the post tests.
	  @results['apt_restore_test'] = run('apt_restore_post_test.go')
	  @results['apt_restore_test'] = run('apt_delete_post_test.go')
	end
  end

  # apt_delete runs the APTrust file deletion service to delete a
  # number of GenericFiles from the archive.
  def apt_delete(more_tests_follow)
	puts 'apt_delete is not yet implemented'
	return true
  end

  # dpn_rest_client tests the DPN REST client against a
  # locally-running DPN cluster. Returns true if all tests passed,
  # false otherwise.
  def dpn_rest_client(more_tests_follow)
	run_suite(more_tests_follow) do
	  @service.dpn_cluster_start
	  @results['dpn_rest_client_test'] = run_dpn_rest_client_test
	end
  end

  # dpn_sync tests the dpn_sync app against a locally-running
  # DPN cluster. dpn_sync runs as a cron job in our staging and
  # production environments, and exits on its own when it's done.
  # The DPN sync post test checks to ensure that all remote records
  # were synched as expected to the local node. Returns true/false
  # to indicate whether all tests passed.
  def dpn_sync(more_tests_follow)
	run_suite(more_tests_follow) do
	  # Build
	  @build.build(@context.apps['dpn_sync'])

	  # Run prerequisites
	  if !apt_ingest(true)
		puts "Skipping dpn_sync test because of prior failures."
		return false
	  end

	  # Start services
	  @service.dpn_cluster_start  # sleeps to wait for all nodes to come up
	  @service.app_start(@context.apps['dpn_sync'])

	  # Post test
	  @results['dpn_sync_test'] = run('dpn_sync_post_test.go')
	end
  end

  # dpn_queue tests the dpn_queue application, which is responsible
  # for finding and queueing 1) replication requests recently synched
  # to our local DPN node that APTrust is responsible for fulfilling
  # (i.e. APTrust is the to_node in those requests), and 2) WorkItems
  # in Pharos that request an APTrust bag be pushed to DPN. Those are
  # DPN ingests performed by APTrust. This test just checks to see that
  # dpn_queue actually finds and queues all the right items.
  #
  # This runs apt_fetch, apt_store, and apt_record before dpn_queue,
  # because we need to ingest the APTrust bags that we're going to
  # mark for DPN.
  def dpn_queue(more_tests_follow)
	run_suite(more_tests_follow) do
	  # Build
	  @build.build(@context.apps['dpn_queue'])

	  # Run prerequisites.
	  dpn_sync_ok = dpn_sync(true)
	  if !dpn_sync_ok
		puts "Skipping dpn_queue test because of prior failures."
		return false
	  end

	  # Push some APTrust bags to DPN. We want to make sure
	  # that dpn_queue picks these up.
	  @results['apt_push_to_dpn'] = run('apt_push_to_dpn_test.go')
	  if @results['apt_push_to_dpn'] == false
		puts "Skipping dpn_queue test because apt_push_to_dpn failed."
		return false
	  end

	  # Start services
	  @service.app_start(@context.apps['dpn_queue'])

	  # Run the post test
	  @results['dpn_queue_test'] = run('dpn_queue_post_test.go')
	end
  end

  # dpn_copy is the first step of the DPN replication process.
  # This copies tarred bags from remote nodes into our staging
  # area. This is run as part of the dpn_replicate suite.
  def dpn_copy(more_tests_follow)
	run_suite(more_tests_follow) do
	  # Build
	  @build.build(@context.apps['dpn_copy'])

	  # Run prerequisites
	  queue_ok = dpn_queue(true)
	  if !queue_ok
		puts "Skipping dpn_copy test because of prior failures."
		return false
	  end

	  # Start service
	  @service.app_start(@context.apps['dpn_copy'])
	  sleep 30

	  # Run the post test
	  @results['dpn_copy_test'] = run('dpn_copy_post_test.go')
	end
  end

  # dpn_validate is the second step of the replication process. We
  # validate bags copied from remote nodes before we put them into
  # long-term storage. This is run as part of the dpn_replicate suite.
  def dpn_validate(more_tests_follow)
	run_suite(more_tests_follow) do
	  # Build
	  @build.build(@context.apps['dpn_validate'])

	  # Run prerequisites
	  copy_ok = dpn_copy(true)
	  if !copy_ok
		puts "Skipping dpn_validate test because of prior failures."
		return false
	  end

	  # Start service
	  @service.app_start(@context.apps['dpn_validate'])
	  sleep 20

	  # Ensure expected post conditions
	  @results['dpn_validate_test'] = run('dpn_validate_post_test.go')
	end
  end

  # dpn_replication_store is the last step of the DPN replication process.
  # It stores DPN bags in S3 and tells the remote ingest node that the bag
  # has been stored. This runs as part of the dpn_replicate suite.
  def dpn_replication_store(more_tests_follow)
	run_suite(more_tests_follow) do
	  # Build
	  @build.build(@context.apps['dpn_replication_store'])

	  # Run prerequisites
	  validate_ok = dpn_validate(true)
	  if !validate_ok
		puts "Skipping dpn_replication_store test because of prior failures."
		return false
	  end

	  # Start service
	  @service.app_start(@context.apps['dpn_replication_store'])
	  sleep 20

	  # Ensure expected post conditions
	  @results['dpn_replication_store_test'] = run('dpn_replication_store_post_test.go')
	end
  end

  # This runs the entire DPN replication suite, and is available
  # from the command line.
  def dpn_replicate(more_tests_follow)
	return dpn_replication_store(more_tests_follow)
  end

  # dpn_package is the first step of the DPN ingest process, packaging
  # an APTrust bag for ingest into DPN. This runs as part of the
  # dpn_ingest suite.
  def dpn_package(more_tests_follow)
	run_suite(more_tests_follow) do
	  # Build
	  @build.build(@context.apps['dpn_package'])

	  # Run prerequisites
	  queue_ok = dpn_queue(true)
	  if !queue_ok
		puts "Skipping dpn_package test because of prior failures."
		return false
	  end

	  # Start service
	  @service.app_start(@context.apps['dpn_package'])
	  sleep 50

	  # Run the post test
	  @results['dpn_package_test'] = run('dpn_package_post_test.go')
	end
  end

  # dpn_ingest_store copies locally ingested DPN bags to long-term
  # storage in AWS Glacier. This is the second step of DPN ingest,
  # after dpn_package. This runs as part of the dpn_ingest suite.
  def dpn_ingest_store(more_tests_follow)
	run_suite(more_tests_follow) do
	  # Build
	  @build.build(@context.apps['dpn_ingest_store'])

	  # Run prerequisites
	  package_ok = dpn_package(true)
	  if !package_ok
		puts "Skipping dpn_ingest_store test because of prior failures."
		return false
	  end

	  # Start service
	  @service.app_start(@context.apps['dpn_ingest_store'])
	  sleep 50

	  # Run the post test
	  @results['dpn_ingest_store_test'] = run('dpn_ingest_store_post_test.go')
	end
  end

  # dpn_ingest_record is the last step of the DPN ingest process. It
  # records info about the newly ingested bag in Pharos and the local
  # DPN REST server. This runs as part of the dpn_ingest suite.
  def dpn_ingest_record(more_tests_follow)
	run_suite(more_tests_follow) do
	  # Build
	  @build.build(@context.apps['dpn_ingest_record'])

	  # Run prerequisites
	  package_ok = dpn_ingest_store(true)
	  if !package_ok
		puts "Skipping dpn_ingest_record test because of prior failures."
		return false
	  end

	  # Start service
	  @service.app_start(@context.apps['dpn_ingest_record'])
	  sleep 30

	  # Run the post test
	  @results['dpn_ingest_record_test'] = run('dpn_ingest_record_post_test.go')
	end
  end

  # This is available from the command line and runs the entire
  # dpn_ingest suite.
  def dpn_ingest(more_tests_follow)
	return dpn_ingest_record(more_tests_follow)
  end

  # Runs all the APTrust and DPN unit tests. Does not run any tests that
  # rely on external services. Returns true/false to indicate whether all
  # tests passed.
  def units(more_tests_follow)
	@results['unit_tests'] = run_all_unit_tests
	print_results
  end

  private

  def print_exception(ex)
	puts ex
	puts ex.backtrace
  end

  # print_results prints the results of each test that was run
  # and returns true if all tests passed, false if any test failed.
  def print_results
	all_tests_passed = true
	puts "\n---Results---"
	@results.each do |test_name, passed|
	  if passed
		message = 'PASS'
	  else
		message = 'FAIL'
		all_tests_passed = false
	  end
	  printf("%-30s: %s\n", test_name, message)
	end
	puts "\n"
	return all_tests_passed
  end

  def all_tests_passed?
	@results.each do |test_name, passed|
	  return false unless passed
	end
	return true
  end

  # run_all_unit_tests runs all of the APTrust and DPN unit tests.
  # These tests do not require any outside services to run, and
  # they omit a handful of tests that do require outside services.
  def run_all_unit_tests
	env = @context.env_hash
	cmd = "go test $(go list ./... | grep -v /vendor/)"
	pid = Process.spawn(env, cmd, chdir: @context.exchange_root)
	Process.wait pid
	return $?.exitstatus == 0
  end

  # dpn_rest_client test runs our Go DPN REST client against a locally-running
  # DPN cluster. The DPN REST client is in exchange/dpn/network.
  # Returns true if tests passed.
  def run_dpn_rest_client_test
	env = @context.env_hash
	cmd = "go test dpn_rest_client_test.go"
	dir = "#{@context.exchange_root}/dpn/network"
	pid = Process.spawn(env, cmd, chdir: dir)
	Process.wait pid
	return $?.exitstatus == 0
  end

  # Runs the specified integration test, setting up the necessary
  # environment first.
  def run(test_file)
	env = @context.env_hash
	env['RUN_EXCHANGE_INTEGRATION'] = 'true'
	dir = "#{@context.exchange_root}/integration"
	cmd = "go test #{test_file}"
	pid = Process.spawn(env, cmd, chdir: dir)
	Process.wait pid
	return $?.exitstatus == 0
  end

  # run_suite runs the suite of commands in the given block.
  # That usually includes building binaries, starting services,
  # and running tests.
  def run_suite(more_tests_follow, &block)
	begin
	  yield
	rescue Exception => ex
	  print_exception(ex)
	  return false
	ensure
	  @service.stop_everything unless more_tests_follow
	end
	if more_tests_follow
	  return all_tests_passed?
	else
	  return print_results
	end
  end

end
