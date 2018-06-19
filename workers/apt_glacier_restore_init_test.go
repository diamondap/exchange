package workers_test

import (
	"encoding/json"
	"fmt"
	"github.com/APTrust/exchange/constants"
	"github.com/APTrust/exchange/models"
	"github.com/APTrust/exchange/network"
	"github.com/APTrust/exchange/util/testutil"
	"github.com/APTrust/exchange/workers"
	"github.com/nsqio/go-nsq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The following package-level vars tell the HTTP test handlers
// how to behave when we make requests. We have to do this because
// we don't have control over the requests themselves, which are
// generated by other libraries.
var NumberOfRequestsToIncludeInState = 0

const (
	NotStartedHead      = 0
	NotStartedAcceptNow = 1
	NotStartedRejectNow = 2
	InProgressHead      = 3
	InProgressGlacier   = 4
	Completed           = 5
)

var DescribeRestoreStateAs = NotStartedHead

const TEST_ID = 1000

var updatedWorkItem = &models.WorkItem{}
var updatedWorkItemState = &models.WorkItemState{}

// Regex to extract ID from URL
var URL_ID_REGEX = regexp.MustCompile(`\/(\d+)\/`)

// Test server to handle Pharos requests
var pharosTestServer = httptest.NewServer(http.HandlerFunc(pharosHandler))

// Test server to handle S3 requests
var s3TestServer = httptest.NewServer(http.HandlerFunc(s3Handler))

func getGlacierRestoreWorker(t *testing.T) *workers.APTGlacierRestoreInit {
	_context, err := testutil.GetContext("integration.json")
	require.Nil(t, err)
	return workers.NewGlacierRestore(_context)
}

func getObjectWorkItem(id int, objectIdentifier string) *models.WorkItem {
	workItemStateId := 1000
	return &models.WorkItem{
		Id:                    id,
		ObjectIdentifier:      objectIdentifier,
		GenericFileIdentifier: "",
		Name:             "glacier_bag.tar",
		Bucket:           "aptrust.receiving.test.edu",
		ETag:             "0000000000000000",
		BagDate:          testutil.RandomDateTime(),
		InstitutionId:    33,
		User:             "frank.zappa@example.com",
		Date:             testutil.RandomDateTime(),
		Note:             "",
		Action:           constants.ActionGlacierRestore,
		Stage:            constants.StageRequested,
		Status:           constants.StatusPending,
		Outcome:          "",
		Retry:            true,
		Node:             "",
		Pid:              0,
		NeedsAdminReview: false,
		WorkItemStateId:  &workItemStateId,
	}
}

func getFileWorkItem(id int, objectIdentifier, fileIdentifier string) *models.WorkItem {
	workItem := getObjectWorkItem(id, objectIdentifier)
	workItem.GenericFileIdentifier = fileIdentifier
	return workItem
}

func getPharosClientForTest(url string) *network.PharosClient {
	client, _ := network.NewPharosClient(url, "v2", "frankzappa", "abcxyz")
	return client
}

func getTestComponents(t *testing.T, fileOrObject string) (*workers.APTGlacierRestoreInit, *models.GlacierRestoreState) {
	worker := getGlacierRestoreWorker(t)
	require.NotNil(t, worker)

	// Tell the worker to talk to our S3 test server and Pharos
	// test server, defined below
	worker.S3Url = s3TestServer.URL
	worker.Context.PharosClient = getPharosClientForTest(pharosTestServer.URL)

	// Set up the GlacierRestoreStateObject
	objIdentifier := "test.edu/glacier_bag"

	// Note that we're getting a WorkItem that has a GenericFileIdentifier
	var workItem *models.WorkItem
	if fileOrObject == "object" {
		workItem = getObjectWorkItem(TEST_ID, objIdentifier)
	} else {
		workItem = getFileWorkItem(TEST_ID, objIdentifier, objIdentifier+"/file1.txt")
	}
	nsqMessage := testutil.MakeNsqMessage(fmt.Sprintf("%d", TEST_ID))

	state, err := worker.GetGlacierRestoreState(nsqMessage, workItem)
	require.Nil(t, err)
	require.NotNil(t, state)
	return worker, state
}

// ------ TESTS --------

func TestNewGlacierRestore(t *testing.T) {
	glacierRestore := getGlacierRestoreWorker(t)
	require.NotNil(t, glacierRestore)
	assert.NotNil(t, glacierRestore.Context)
	assert.NotNil(t, glacierRestore.RequestChannel)
	assert.NotNil(t, glacierRestore.CleanupChannel)
}

func TestGetGlacierRestoreState(t *testing.T) {
	worker, state := getTestComponents(t, "object")

	NumberOfRequestsToIncludeInState = 0
	worker.Context.PharosClient = getPharosClientForTest(pharosTestServer.URL)
	state, err := worker.GetGlacierRestoreState(state.NSQMessage, state.WorkItem)
	require.Nil(t, err)
	require.NotNil(t, state)
	assert.NotNil(t, state.WorkSummary)
	assert.Empty(t, state.Requests)

	NumberOfRequestsToIncludeInState = 10
	state, err = worker.GetGlacierRestoreState(state.NSQMessage, state.WorkItem)
	require.Nil(t, err)
	require.NotNil(t, state)
	assert.NotNil(t, state.WorkSummary)
	require.NotEmpty(t, state.Requests)
	assert.Equal(t, NumberOfRequestsToIncludeInState, len(state.Requests))
}

func TestRequestObject(t *testing.T) {
	NumberOfRequestsToIncludeInState = 0
	DescribeRestoreStateAs = NotStartedHead

	worker, state := getTestComponents(t, "object")
	require.Nil(t, state.IntellectualObject)
	worker.RequestObject(state)
	require.NotNil(t, state.IntellectualObject)
	require.NotEmpty(t, state.IntellectualObject.GenericFiles)
	// Should be 12 of each
	assert.Equal(t, len(state.IntellectualObject.GenericFiles), len(state.Requests))
	for _, req := range state.Requests {
		assert.NotEmpty(t, req.GenericFileIdentifier)
		assert.NotEmpty(t, req.GlacierBucket)
		assert.NotEmpty(t, req.GlacierKey)
		assert.False(t, req.RequestedAt.IsZero())
		assert.False(t, req.RequestAccepted)
		assert.False(t, req.IsAvailableInS3)
	}

	DescribeRestoreStateAs = NotStartedAcceptNow
	worker, state = getTestComponents(t, "object")
	require.Nil(t, state.IntellectualObject)
	worker.RequestObject(state)
	assert.Equal(t, len(state.IntellectualObject.GenericFiles), len(state.Requests))
	for _, req := range state.Requests {
		assert.NotEmpty(t, req.GenericFileIdentifier)
		assert.NotEmpty(t, req.GlacierBucket)
		assert.NotEmpty(t, req.GlacierKey)
		assert.False(t, req.RequestedAt.IsZero())
		assert.True(t, req.RequestAccepted)
		assert.False(t, req.IsAvailableInS3)
	}
}

func TestRestoreRequestNeeded(t *testing.T) {
	worker, state := getTestComponents(t, "object")
	require.Nil(t, state.IntellectualObject)

	// Now let's check to see if we need to issue a Glacier restore
	// request for the following file. Tell the s3 test server to
	// reply that this restore has not been requested yet for this item.
	DescribeRestoreStateAs = NotStartedHead
	gf := testutil.MakeGenericFile(0, 0, state.WorkItem.ObjectIdentifier)
	fileUUID, _ := gf.PreservationStorageFileName()
	requestNeeded, err := worker.RestoreRequestNeeded(state, gf)
	require.Nil(t, err)
	assert.True(t, requestNeeded)

	// Make sure the GlacierRestore worker created a
	// GlacierRestoreRequest record for this file.
	// In the test environment, glacierRestoreRequest.GlacierBucket
	// will be an empty string.
	glacierRestoreRequest := state.FindRequest(gf.Identifier)
	require.NotNil(t, glacierRestoreRequest)
	assert.Equal(t, fileUUID, glacierRestoreRequest.GlacierKey)
	// Request cannot have been accepted, because it hasn't been issued.
	assert.False(t, glacierRestoreRequest.RequestAccepted)
	assert.False(t, glacierRestoreRequest.IsAvailableInS3)
	assert.False(t, glacierRestoreRequest.SomeoneElseRequested)
	assert.True(t, glacierRestoreRequest.RequestedAt.IsZero())
	assert.WithinDuration(t, time.Now().UTC(), glacierRestoreRequest.LastChecked, 10*time.Second)

	// Check to see if we need to issue a Glacier restore
	// request for a file that we've already requested and whose
	// restoration is currently in progress. Tell the s3 test server to
	// reply that restore is in progress for this item.
	DescribeRestoreStateAs = InProgressHead
	gf = testutil.MakeGenericFile(0, 0, state.WorkItem.ObjectIdentifier)
	fileUUID, _ = gf.PreservationStorageFileName()
	requestNeeded, err = worker.RestoreRequestNeeded(state, gf)
	require.Nil(t, err)
	assert.False(t, requestNeeded)

	// Make sure the GlacierRestore worker created a
	// GlacierRestoreRequest record for this file.
	glacierRestoreRequest = state.FindRequest(gf.Identifier)
	require.NotNil(t, glacierRestoreRequest)
	assert.Equal(t, fileUUID, glacierRestoreRequest.GlacierKey)
	// Request must have been accepted, because the restore is in progress.
	assert.True(t, glacierRestoreRequest.RequestAccepted)
	assert.False(t, glacierRestoreRequest.IsAvailableInS3)
	assert.False(t, glacierRestoreRequest.SomeoneElseRequested)
	assert.False(t, glacierRestoreRequest.RequestedAt.IsZero())
	assert.WithinDuration(t, time.Now().UTC(), glacierRestoreRequest.LastChecked, 10*time.Second)

	// Check to see if we need to issue a Glacier restore
	// request for a file that's already been restored to S3.
	// Tell the s3 test server to reply that restore is complete for this item.
	DescribeRestoreStateAs = Completed
	gf = testutil.MakeGenericFile(0, 0, state.WorkItem.ObjectIdentifier)
	fileUUID, _ = gf.PreservationStorageFileName()
	requestNeeded, err = worker.RestoreRequestNeeded(state, gf)
	require.Nil(t, err)
	assert.False(t, requestNeeded)

	// Make sure the GlacierRestore worker created a
	// GlacierRestoreRequest record for this file.
	glacierRestoreRequest = state.FindRequest(gf.Identifier)
	require.NotNil(t, glacierRestoreRequest)
	assert.Equal(t, fileUUID, glacierRestoreRequest.GlacierKey)
	// Request must have been accepted, because the restore is in progress.
	assert.True(t, glacierRestoreRequest.RequestAccepted)
	assert.True(t, glacierRestoreRequest.IsAvailableInS3)
	assert.False(t, glacierRestoreRequest.SomeoneElseRequested)
	assert.False(t, glacierRestoreRequest.RequestedAt.IsZero())
	assert.False(t, glacierRestoreRequest.EstimatedDeletionFromS3.IsZero())
	assert.WithinDuration(t, time.Now().UTC(), glacierRestoreRequest.LastChecked, 10*time.Second)
}

func TestGetS3HeadClient(t *testing.T) {
	worker := getGlacierRestoreWorker(t)
	require.NotNil(t, worker)

	// Standard
	client, err := worker.GetS3HeadClient(constants.StorageStandard)
	require.Nil(t, err)
	require.NotNil(t, client)
	assert.Equal(t, worker.Context.Config.APTrustS3Region, client.AWSRegion)
	assert.Equal(t, worker.Context.Config.PreservationBucket, client.BucketName)

	// Glacier OH
	client, err = worker.GetS3HeadClient(constants.StorageGlacierOH)
	require.Nil(t, err)
	require.NotNil(t, client)
	assert.Equal(t, worker.Context.Config.GlacierRegionOH, client.AWSRegion)
	assert.Equal(t, worker.Context.Config.GlacierBucketOH, client.BucketName)

	// Glacier OR
	client, err = worker.GetS3HeadClient(constants.StorageGlacierOR)
	require.Nil(t, err)
	require.NotNil(t, client)
	assert.Equal(t, worker.Context.Config.GlacierRegionOR, client.AWSRegion)
	assert.Equal(t, worker.Context.Config.GlacierBucketOR, client.BucketName)

	// Glacier VA
	client, err = worker.GetS3HeadClient(constants.StorageGlacierVA)
	require.Nil(t, err)
	require.NotNil(t, client)
	assert.Equal(t, worker.Context.Config.GlacierRegionVA, client.AWSRegion)
	assert.Equal(t, worker.Context.Config.GlacierBucketVA, client.BucketName)
}

func TestGetIntellectualObject(t *testing.T) {
	worker, state := getTestComponents(t, "object")
	require.Nil(t, state.IntellectualObject)

	require.Nil(t, state.IntellectualObject)

	obj, err := worker.GetIntellectualObject(state)
	assert.Nil(t, err)
	require.NotNil(t, obj)
	assert.Equal(t, 12, len(obj.GenericFiles))
}

func TestGetGenericFile(t *testing.T) {
	worker, state := getTestComponents(t, "file")
	require.Nil(t, state.GenericFile)

	state, err := worker.GetGlacierRestoreState(state.NSQMessage, state.WorkItem)
	require.Nil(t, err)
	require.NotNil(t, state)
	require.Nil(t, state.GenericFile)

	gf, err := worker.GetGenericFile(state)
	assert.Nil(t, err)
	require.NotNil(t, gf)
	assert.NotEmpty(t, gf.Identifier)
	assert.NotEmpty(t, gf.StorageOption)
	assert.NotEmpty(t, gf.URI)
}

func TestUpdateWorkItem(t *testing.T) {
	worker, state := getTestComponents(t, "object")

	state.WorkItem.Note = "Updated note"
	state.WorkItem.Node = "blah-blah-blah"
	state.WorkItem.Pid = 9800
	state.WorkItem.Status = constants.StatusSuccess

	worker.UpdateWorkItem(state)
	assert.Empty(t, state.WorkSummary.Errors)
	assert.Equal(t, "Updated note", updatedWorkItem.Note)
	assert.Equal(t, "blah-blah-blah", updatedWorkItem.Node)
	assert.Equal(t, 9800, updatedWorkItem.Pid)
	assert.Equal(t, constants.StatusSuccess, updatedWorkItem.Status)
}

func TestSaveWorkItemState(t *testing.T) {
	worker, state := getTestComponents(t, "object")
	requestCount := len(state.Requests)
	for i := 0; i < 10; i++ {
		request := &models.GlacierRestoreRequest{
			GenericFileIdentifier: fmt.Sprintf("%s/file%d.txt", state.WorkItem.ObjectIdentifier, i),
		}
		state.Requests = append(state.Requests, request)
	}
	worker.SaveWorkItemState(state)
	require.NotNil(t, updatedWorkItemState)
	require.True(t, updatedWorkItemState.HasData())
	glacierRestoreState, err := updatedWorkItemState.GlacierRestoreState()
	require.Nil(t, err)
	assert.Equal(t, requestCount+10, len(glacierRestoreState.Requests))
}

func TestFinishWithError(t *testing.T) {
	worker, state := getTestComponents(t, "object")
	delegate := NewNSQTestDelegate()
	state.NSQMessage.Delegate = delegate
	state.WorkSummary.AddError("Error 1")
	state.WorkSummary.AddError("Error 2")
	worker.FinishWithError(state)
	assert.Equal(t, "finish", delegate.Operation)
	assert.Equal(t, state.WorkSummary.AllErrorsAsString(), state.WorkItem.Note)
	assert.Equal(t, constants.StatusFailed, state.WorkItem.Status)
	assert.False(t, state.WorkItem.Retry)
	assert.True(t, state.WorkItem.NeedsAdminReview)
}

func TestRequeueForAdditionalRequests(t *testing.T) {
	worker, state := getTestComponents(t, "object")
	delegate := NewNSQTestDelegate()
	state.NSQMessage.Delegate = delegate
	worker.RequeueForAdditionalRequests(state)
	assert.Equal(t, "requeue", delegate.Operation)
	assert.Equal(t, 1*time.Minute, delegate.Delay)
	assert.Equal(t, "Requeued to make additional Glacier restore requests.", state.WorkItem.Note)
	assert.Equal(t, constants.StatusStarted, state.WorkItem.Status)
	assert.True(t, state.WorkItem.Retry)
	assert.False(t, state.WorkItem.NeedsAdminReview)
}

func TestRequeueToCheckState(t *testing.T) {
	worker, state := getTestComponents(t, "object")
	delegate := NewNSQTestDelegate()
	state.NSQMessage.Delegate = delegate
	worker.RequeueToCheckState(state)
	assert.Equal(t, "requeue", delegate.Operation)
	assert.Equal(t, 2*time.Hour, delegate.Delay)
	assert.Equal(t, "Requeued to check on status of Glacier restore requests.", state.WorkItem.Note)
	assert.Equal(t, constants.StatusStarted, state.WorkItem.Status)
	assert.True(t, state.WorkItem.Retry)
	assert.False(t, state.WorkItem.NeedsAdminReview)
}

func TestCreateRestoreWorkItem(t *testing.T) {
	worker, state := getTestComponents(t, "object")
	delegate := NewNSQTestDelegate()
	state.NSQMessage.Delegate = delegate
	worker.CreateRestoreWorkItem(state)
	assert.Equal(t, "finish", delegate.Operation)
	assert.Equal(t, constants.StatusSuccess, state.WorkItem.Status)

	assert.Equal(t, state.WorkItem.ObjectIdentifier, updatedWorkItem.ObjectIdentifier)
	assert.Equal(t, state.WorkItem.GenericFileIdentifier, updatedWorkItem.GenericFileIdentifier)
	assert.Equal(t, state.WorkItem.Name, updatedWorkItem.Name)
	assert.Equal(t, state.WorkItem.Bucket, updatedWorkItem.Bucket)
	assert.Equal(t, state.WorkItem.ETag, updatedWorkItem.ETag)
	assert.Equal(t, state.WorkItem.Size, updatedWorkItem.Size)
	assert.Equal(t, state.WorkItem.BagDate, updatedWorkItem.BagDate)
	assert.Equal(t, state.WorkItem.InstitutionId, updatedWorkItem.InstitutionId)
	assert.Equal(t, state.WorkItem.User, updatedWorkItem.User)
	assert.Equal(t, constants.ActionRestore, updatedWorkItem.Action)
	assert.Equal(t, constants.StageRequested, updatedWorkItem.Stage)
	assert.Equal(t, constants.StatusPending, updatedWorkItem.Status)
	assert.True(t, updatedWorkItem.Retry)
}

func TestRequestAllFiles(t *testing.T) {
	NumberOfRequestsToIncludeInState = 0
	DescribeRestoreStateAs = NotStartedAcceptNow

	worker, state := getTestComponents(t, "object")
	state.IntellectualObject = testutil.MakeIntellectualObject(12, 0, 0, 0)
	DescribeRestoreStateAs = NotStartedAcceptNow
	worker.RequestAllFiles(state)
	assert.Empty(t, state.WorkSummary.Errors)
	assert.NotNil(t, state.IntellectualObject)
	assert.Equal(t, 12, len(state.Requests))
	for _, req := range state.Requests {
		assert.NotEmpty(t, req.GenericFileIdentifier)
		assert.NotEmpty(t, req.GlacierBucket)
		assert.NotEmpty(t, req.GlacierKey)
		assert.False(t, req.RequestedAt.IsZero())
		assert.True(t, req.RequestAccepted)
		assert.False(t, req.IsAvailableInS3)
	}
}

func TestRequestFile(t *testing.T) {
	worker, state := getTestComponents(t, "file")
	delegate := NewNSQTestDelegate()
	state.NSQMessage.Delegate = delegate

	gf, err := worker.GetGenericFile(state)
	assert.Nil(t, err)
	require.NotNil(t, gf)

	// Call RequestFile then check the state of the
	// GlacierRestoreRequest for that file.
	DescribeRestoreStateAs = NotStartedRejectNow
	worker.RequestFile(state, gf)
	glacierRestoreRequest := worker.GetRequestRecord(state, gf, make(map[string]string))
	timeOfFirstRequest := glacierRestoreRequest.RequestedAt
	require.NotNil(t, glacierRestoreRequest)
	assert.False(t, glacierRestoreRequest.RequestedAt.IsZero())
	assert.True(t, glacierRestoreRequest.LastChecked.IsZero())
	assert.False(t, glacierRestoreRequest.RequestAccepted)
	assert.False(t, glacierRestoreRequest.IsAvailableInS3)

	// Now accept the request and make sure the request record
	// was properly updated.
	DescribeRestoreStateAs = NotStartedAcceptNow
	worker.RequestFile(state, gf)
	glacierRestoreRequest = worker.GetRequestRecord(state, gf, make(map[string]string))
	require.NotNil(t, glacierRestoreRequest)
	assert.True(t, glacierRestoreRequest.LastChecked.IsZero())
	assert.True(t, glacierRestoreRequest.RequestedAt.After(timeOfFirstRequest))
	assert.False(t, glacierRestoreRequest.IsAvailableInS3)

	// Make sure LastChecked is updated when we do a status check
	// via S3 Head on a file whose restoration request was accepted by Glacier.
	DescribeRestoreStateAs = InProgressGlacier
	worker.RequestFile(state, gf)
	glacierRestoreRequest = worker.GetRequestRecord(state, gf, make(map[string]string))
	require.NotNil(t, glacierRestoreRequest)
	assert.True(t, glacierRestoreRequest.LastChecked.IsZero())
	assert.False(t, glacierRestoreRequest.IsAvailableInS3)
}

func TestGetRequestDetails(t *testing.T) {
	worker, state := getTestComponents(t, "file")
	require.Nil(t, state.GenericFile)

	state, err := worker.GetGlacierRestoreState(state.NSQMessage, state.WorkItem)
	require.Nil(t, err)
	require.NotNil(t, state)
	require.Nil(t, state.GenericFile)

	gf, err := worker.GetGenericFile(state)
	assert.Nil(t, err)
	require.NotNil(t, gf)

	fileUUID, err := gf.PreservationStorageFileName()
	require.Nil(t, err)

	// Glacier Ohio
	gf.StorageOption = constants.StorageGlacierOH
	details, err := worker.GetRequestDetails(gf)
	require.Nil(t, err)
	require.NotNil(t, details)
	assert.Equal(t, fileUUID, details["fileUUID"])
	assert.Equal(t, worker.Context.Config.GlacierRegionOH, details["region"])
	assert.Equal(t, worker.Context.Config.GlacierBucketOH, details["bucket"])

	// Glacier Oregon
	gf.StorageOption = constants.StorageGlacierOR
	details, err = worker.GetRequestDetails(gf)
	require.Nil(t, err)
	require.NotNil(t, details)
	assert.Equal(t, fileUUID, details["fileUUID"])
	assert.Equal(t, worker.Context.Config.GlacierRegionOR, details["region"])
	assert.Equal(t, worker.Context.Config.GlacierBucketOR, details["bucket"])

	// Glacier Virginia
	gf.StorageOption = constants.StorageGlacierVA
	details, err = worker.GetRequestDetails(gf)
	require.Nil(t, err)
	require.NotNil(t, details)
	assert.Equal(t, fileUUID, details["fileUUID"])
	assert.Equal(t, worker.Context.Config.GlacierRegionVA, details["region"])
	assert.Equal(t, worker.Context.Config.GlacierBucketVA, details["bucket"])

	// Standard storage
	gf.StorageOption = constants.StorageStandard
	details, err = worker.GetRequestDetails(gf)
	require.Nil(t, err)
	require.NotNil(t, details)
	assert.Equal(t, fileUUID, details["fileUUID"])
	assert.Equal(t, worker.Context.Config.APTrustGlacierRegion, details["region"])
	assert.Equal(t, worker.Context.Config.ReplicationBucket, details["bucket"])

	// Bogus storage - should cause error
	gf.StorageOption = "ThumbDrive"
	details, err = worker.GetRequestDetails(gf)
	require.NotNil(t, err)
	require.Nil(t, details)
}

func TestGetRequestRecord(t *testing.T) {
	worker, state := getTestComponents(t, "file")
	require.Nil(t, state.GenericFile)

	state, err := worker.GetGlacierRestoreState(state.NSQMessage, state.WorkItem)
	require.Nil(t, err)
	require.NotNil(t, state)
	require.Nil(t, state.GenericFile)

	gf, err := worker.GetGenericFile(state)
	assert.Nil(t, err)
	require.NotNil(t, gf)
	assert.NotEmpty(t, gf.Identifier)

	gf.StorageOption = constants.StorageGlacierOH
	details, err := worker.GetRequestDetails(gf)
	require.Nil(t, err)
	require.NotNil(t, details)

	fileUUID, err := gf.PreservationStorageFileName()
	require.Nil(t, err)

	// Should create a new request with correct information
	glacierRestoreRequest := worker.GetRequestRecord(state, gf, details)
	assert.Equal(t, gf.Identifier, glacierRestoreRequest.GenericFileIdentifier)
	assert.Equal(t, worker.Context.Config.GlacierBucketOH, glacierRestoreRequest.GlacierBucket)
	assert.Equal(t, fileUUID, glacierRestoreRequest.GlacierKey)
	assert.False(t, glacierRestoreRequest.RequestAccepted)
	assert.True(t, glacierRestoreRequest.RequestedAt.IsZero())

	// Should retrieve an exising request.
	gf = testutil.MakeGenericFile(0, 0, "test.edu/bag-of-glass")
	request := &models.GlacierRestoreRequest{
		GenericFileIdentifier: gf.Identifier,
		GlacierBucket:         "6-piece fried chicken bucket",
		GlacierKey:            "extra crispy",
		RequestAccepted:       true,
		SomeoneElseRequested:  true,
	}
	state.Requests = append(state.Requests, request)
	glacierRestoreRequest = worker.GetRequestRecord(state, gf, details)
	assert.Equal(t, request.GenericFileIdentifier, glacierRestoreRequest.GenericFileIdentifier)
	assert.Equal(t, request.GlacierBucket, glacierRestoreRequest.GlacierBucket)
	assert.Equal(t, request.GlacierKey, glacierRestoreRequest.GlacierKey)
	assert.Equal(t, request.RequestAccepted, glacierRestoreRequest.RequestAccepted)
	assert.Equal(t, request.SomeoneElseRequested, glacierRestoreRequest.SomeoneElseRequested)
}

func TestInitializeRetrieval(t *testing.T) {
	worker, state := getTestComponents(t, "file")
	require.Nil(t, state.GenericFile)

	state, err := worker.GetGlacierRestoreState(state.NSQMessage, state.WorkItem)
	require.Nil(t, err)
	require.NotNil(t, state)
	require.Nil(t, state.GenericFile)

	gf, err := worker.GetGenericFile(state)
	assert.Nil(t, err)
	require.NotNil(t, gf)
	assert.NotEmpty(t, gf.Identifier)
	assert.NotEmpty(t, gf.StorageOption)
	assert.NotEmpty(t, gf.URI)

	details, err := worker.GetRequestDetails(gf)
	require.Nil(t, err)
	require.NotNil(t, details)

	glacierRestoreRequest := worker.GetRequestRecord(state, gf, details)
	assert.False(t, glacierRestoreRequest.RequestAccepted)
	assert.True(t, glacierRestoreRequest.RequestedAt.IsZero())

	// Set our S3 mock responder to accept a Glacier restore request,
	// and then test InitializeRetrieval to ensure it sets
	// properties correctly for an accepted request.
	DescribeRestoreStateAs = NotStartedAcceptNow
	worker.InitializeRetrieval(state, gf, details, glacierRestoreRequest)
	assert.Empty(t, state.WorkSummary.Errors)
	assert.True(t, glacierRestoreRequest.RequestAccepted)
	assert.False(t, glacierRestoreRequest.RequestedAt.IsZero())

	// Reset these properties...
	glacierRestoreRequest.RequestAccepted = false
	glacierRestoreRequest.RequestedAt = time.Time{}

	// And then make sure InitializeRetrieval sets them correctly
	// on a restore that's already in progress.
	DescribeRestoreStateAs = InProgressGlacier
	worker.InitializeRetrieval(state, gf, details, glacierRestoreRequest)
	assert.Empty(t, state.WorkSummary.Errors)
	assert.True(t, glacierRestoreRequest.RequestAccepted)
	assert.False(t, glacierRestoreRequest.RequestedAt.IsZero())
}

// -------------------------------------------------------------------------
// TODO: End-to-end test with the following:
//
// 1. IntellectualObject where all requests succeed.
// 2. IntellectualObject where some requests do not succeed.
//    This should be requeued for retry.
// 3. GenericFile where request succeeds.
// 4. GenericFile where request fails (and is retried).
//
// TODO: Mocks for the following...
//
// 1. Glacier restore request
// 2. S3 head request
// 3. NSQ requeue
//
// Will need a customized Context object where URLs for NSQ,
// Pharos, S3, and Glacier point to the mock services.
// -------------------------------------------------------------------------

func TestGlacierNotStarted(t *testing.T) {
	NumberOfRequestsToIncludeInState = 0
	DescribeRestoreStateAs = NotStartedHead

	worker, state := getTestComponents(t, "object")
	state.IntellectualObject = testutil.MakeIntellectualObject(12, 0, 0, 0)
	delegate := NewNSQTestDelegate()
	state.NSQMessage.Delegate = delegate

	// Create a post-test channel to check the state of various
	// items after they've gone through the entire workflow.
	worker.PostTestChannel = make(chan *models.GlacierRestoreState)
	var wg sync.WaitGroup
	go func() {
		wg.Add(1)
		for state := range worker.PostTestChannel {
			assert.Empty(t, state.WorkSummary.Errors)
			assert.NotNil(t, state.IntellectualObject)
			assert.Equal(t, 12, len(state.Requests))
			for _, req := range state.Requests {
				assert.NotEmpty(t, req.GenericFileIdentifier)
				assert.NotEmpty(t, req.GlacierBucket)
				assert.NotEmpty(t, req.GlacierKey)
				assert.False(t, req.RequestedAt.IsZero())
				assert.True(t, req.RequestAccepted)
				assert.False(t, req.IsAvailableInS3)
			}
			assert.Equal(t, "requeue", delegate.Operation)
			assert.Equal(t, 1*time.Minute, delegate.Delay)
			assert.Equal(t, "Requeued to make additional Glacier restore requests.", state.WorkItem.Note)
			assert.Equal(t, constants.StatusStarted, state.WorkItem.Status)
			assert.True(t, state.WorkItem.Retry)
			assert.False(t, state.WorkItem.NeedsAdminReview)
			wg.Done()
		}
	}()

	worker.RequestChannel <- state
	wg.Wait()
}

func TestGlacierAcceptNow(t *testing.T) {
	NumberOfRequestsToIncludeInState = 0
	DescribeRestoreStateAs = NotStartedAcceptNow

	worker, state := getTestComponents(t, "object")
	state.IntellectualObject = testutil.MakeIntellectualObject(12, 0, 0, 0)
	delegate := NewNSQTestDelegate()
	state.NSQMessage.Delegate = delegate

	worker.PostTestChannel = make(chan *models.GlacierRestoreState)
	var wg sync.WaitGroup
	go func() {
		wg.Add(1)
		for state := range worker.PostTestChannel {
			assert.Empty(t, state.WorkSummary.Errors)
			assert.NotNil(t, state.IntellectualObject)
			assert.Equal(t, 12, len(state.Requests))
			for _, req := range state.Requests {
				assert.NotEmpty(t, req.GenericFileIdentifier)
				assert.NotEmpty(t, req.GlacierBucket)
				assert.NotEmpty(t, req.GlacierKey)
				assert.False(t, req.RequestedAt.IsZero())
				assert.True(t, req.RequestAccepted)
				assert.False(t, req.IsAvailableInS3)
			}
			assert.Equal(t, "requeue", delegate.Operation)
			assert.Equal(t, 2*time.Hour, delegate.Delay)
			assert.Equal(t, "Requeued to check on status of Glacier restore requests.", state.WorkItem.Note)
			assert.Equal(t, constants.StatusStarted, state.WorkItem.Status)
			assert.True(t, state.WorkItem.Retry)
			assert.False(t, state.WorkItem.NeedsAdminReview)
			wg.Done()
		}
	}()

	worker.RequestChannel <- state
	wg.Wait()
}

func TestGlacierRejectNow(t *testing.T) {
	NumberOfRequestsToIncludeInState = 0
	DescribeRestoreStateAs = NotStartedRejectNow

}

func TestGlacierInProgressHead(t *testing.T) {
	NumberOfRequestsToIncludeInState = 0
	DescribeRestoreStateAs = InProgressHead

}

func TestGlacierInProgressGlacier(t *testing.T) {
	NumberOfRequestsToIncludeInState = 0
	DescribeRestoreStateAs = InProgressGlacier

}

func TestGlacierCompleted(t *testing.T) {
	NumberOfRequestsToIncludeInState = 0
	DescribeRestoreStateAs = Completed

}

// -------------------------------------------------------------------------
// HTTP test handlers
// -------------------------------------------------------------------------

func getRequestData(r *http.Request) (map[string]interface{}, error) {
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	data := make(map[string]interface{})
	err := decoder.Decode(&data)
	return data, err
}

func getIdFromUrl(url string) int {
	id := 1000
	matches := URL_ID_REGEX.FindAllStringSubmatch(url, 1)
	if len(matches[0]) > 0 {
		id, _ = strconv.Atoi(matches[0][1])
	}
	return id
}

func workItemGetHandler(w http.ResponseWriter, r *http.Request) {
	obj := testutil.MakeWorkItem()
	objJson, _ := json.Marshal(obj)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, string(objJson))
}

// Simulate updating of WorkItem. Pharos returns the updated WorkItem,
// so this mock can just return the JSON as-is, and then the test
// code can check that to see whether the worker sent the right data
// to Pharos.
func workItemPutHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Fprintln(w, err.Error())
		return
	}
	_ = json.Unmarshal(body, updatedWorkItem)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, string(body))
}

func workItemStateGetHandler(w http.ResponseWriter, r *http.Request) {
	id := getIdFromUrl(r.URL.String())
	obj := testutil.MakeWorkItemState()
	obj.WorkItemId = id
	obj.Action = constants.ActionGlacierRestore
	obj.State = ""
	state := &models.GlacierRestoreState{}
	state.WorkSummary = testutil.MakeWorkSummary()

	// Add some Glacier request records to this object, if necessary
	for i := 0; i < NumberOfRequestsToIncludeInState; i++ {
		fileIdentifier := fmt.Sprintf("test.edu/glacier_bag/file_%d.pdf", i+1)
		request := testutil.MakeGlacierRestoreRequest(fileIdentifier, true)
		state.Requests = append(state.Requests, request)
	}
	jsonBytes, err := json.Marshal(state)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON data: %v", err)
		fmt.Fprintln(w, err.Error())
		return
	}
	obj.State = string(jsonBytes)

	objJson, _ := json.Marshal(obj)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, string(objJson))
}

// Send back the same JSON we received.
func workItemStatePutHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Fprintln(w, err.Error())
		return
	}
	_ = json.Unmarshal(body, updatedWorkItemState)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, string(body))
}

func intellectualObjectGetHandler(w http.ResponseWriter, r *http.Request) {
	obj := testutil.MakeIntellectualObject(12, 0, 0, 0)
	obj.StorageOption = constants.StorageGlacierOH
	for i, gf := range obj.GenericFiles {
		gf.Identifier = fmt.Sprintf("%s/file_%d.txt", obj.Identifier, i)
		gf.StorageOption = constants.StorageGlacierOH
	}
	objJson, _ := json.Marshal(obj)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, string(objJson))
}

func genericFileGetHandler(w http.ResponseWriter, r *http.Request) {
	obj := testutil.MakeGenericFile(0, 2, "test.edu/glacier_bag")
	obj.StorageOption = constants.StorageGlacierOH
	objJson, _ := json.Marshal(obj)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, string(objJson))
}

// pharosHandler handles all requests that the GlacierRestoreInit
// worker would send to Pharos.
func pharosHandler(w http.ResponseWriter, r *http.Request) {
	url := r.URL.String()
	if strings.Contains(url, "/item_state/") {
		if r.Method == http.MethodGet {
			workItemStateGetHandler(w, r)
		} else {
			workItemStatePutHandler(w, r)
		}
	} else if strings.Contains(url, "/items/") {
		if r.Method == http.MethodGet {
			workItemGetHandler(w, r)
		} else {
			workItemPutHandler(w, r)
		}
	} else if strings.Contains(url, "/objects/") {
		intellectualObjectGetHandler(w, r)
	} else if strings.Contains(url, "/files/") {
		genericFileGetHandler(w, r)
	} else {
		panic(fmt.Sprintf("Don't know how to handle request for %s", url))
	}
}

// s3Handler handles all the requests that the GlacierRestoreInit
// worker would send to S3 (including requests to move Glacier objects
// back into S3).
func s3Handler(w http.ResponseWriter, r *http.Request) {
	if DescribeRestoreStateAs == NotStartedHead {
		// S3 HEAD handler will tell us this item is in Glacier, but not yet S3
		network.S3HeadHandler(w, r)
	} else if DescribeRestoreStateAs == NotStartedAcceptNow {
		// Restore handler accepts a Glacier restore requests
		network.S3RestoreHandler(w, r)
	} else if DescribeRestoreStateAs == NotStartedRejectNow {
		// Reject handler reject a Glacier restore requests
		network.S3RestoreRejectHandler(w, r)
	} else if DescribeRestoreStateAs == InProgressHead {
		// This handler is an S3 call that tells us the Glacier restore
		// is in progress, but not yet complete.
		network.S3HeadRestoreInProgressHandler(w, r)
	} else if DescribeRestoreStateAs == InProgressGlacier {
		// This is a Glacier API call that tells us the restore is
		// in progress, but not yet complete.
		network.S3RestoreInProgressHandler(w, r)
	} else if DescribeRestoreStateAs == Completed {
		// This is an S3 API call, where the HEAD response includes
		// info saying the restore is complete and the item will be
		// available in S3 until a specific date/time.
		network.S3HeadRestoreCompletedHandler(w, r)
	}
}

type NSQTestDelegate struct {
	Message   *nsq.Message
	Delay     time.Duration
	Backoff   bool
	Operation string
}

func NewNSQTestDelegate() *NSQTestDelegate {
	return &NSQTestDelegate{}
}

func (delegate *NSQTestDelegate) OnFinish(message *nsq.Message) {
	delegate.Message = message
	delegate.Operation = "finish"
}

func (delegate *NSQTestDelegate) OnRequeue(message *nsq.Message, delay time.Duration, backoff bool) {
	delegate.Message = message
	delegate.Delay = delay
	delegate.Backoff = backoff
	delegate.Operation = "requeue"
}

func (delegate *NSQTestDelegate) OnTouch(message *nsq.Message) {
	delegate.Message = message
	delegate.Operation = "touch"
}
