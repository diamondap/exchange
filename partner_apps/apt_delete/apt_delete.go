package main

import (
	"flag"
	"fmt"
	"github.com/APTrust/exchange/constants"
	"github.com/APTrust/exchange/network"
	"github.com/APTrust/exchange/partner_apps/common"
	"os"
	"strings"
)

func main() {
	opts, keys := getUserOptions()
	if opts.HasErrors() {
		fmt.Fprintln(os.Stderr, opts.AllErrorsAsString())
		os.Exit(common.EXIT_USER_ERR)
	}
	s3ObjDelete := network.NewS3ObjectDelete(
		opts.AccessKeyId,
		opts.SecretAccessKey,
		opts.Region,
		opts.Bucket,
		keys)
	s3ObjDelete.DeleteList()
	if s3ObjDelete.ErrorMessage != "" {
		os.Exit(printError(s3ObjDelete.ErrorMessage))
	}
	os.Exit(common.EXIT_OK)
}

func printError(errMsg string) int {
	exitCode := common.EXIT_RUNTIME_ERR
	fmt.Fprintln(os.Stderr, errMsg)
	if strings.Contains(errMsg, "AccessDenied") {
		fmt.Fprintln(os.Stderr, "Be sure the bucket and key name are correct. "+
			"S3 may return 'Access Denied' for buckets that don't exist.")
	}
	if strings.Contains(errMsg, "NoSuchKey") {
		exitCode = common.EXIT_ITEM_NOT_FOUND
	}
	return exitCode
}

// Get user-specified options from the command line,
// environment, and/or config file.
func getUserOptions() (*common.Options, []string) {
	opts, keys := parseCommandLine()
	opts.SetAndVerifyDeleteOptions()
	return opts, keys
}

func parseCommandLine() (*common.Options, []string) {
	var pathToConfigFile string
	var region string
	var bucket string
	var key string
	var help bool
	var version bool
	flag.StringVar(&pathToConfigFile, "config", "", "Path to partner config file")
	flag.StringVar(&region, "region", constants.AWSVirginia, "AWS region (default 'us-east-1')")
	flag.StringVar(&bucket, "bucket", "", "The bucket to delete from")
	flag.StringVar(&key, "key", "", "The key (name) of the object to delete")
	flag.BoolVar(&help, "help", false, "Show help")
	flag.BoolVar(&version, "version", false, "Show version")

	flag.Parse()

	if version {
		fmt.Println(common.GetVersion())
		os.Exit(common.EXIT_NO_OP)
	}
	if help {
		printUsage()
		os.Exit(common.EXIT_NO_OP)
	}

	opts := &common.Options{
		PathToConfigFile: pathToConfigFile,
		Region:           region,
		Bucket:           bucket,
		Key:              key,
	}

	if os.Getenv("AWS_ACCESS_KEY_ID") != "" {
		opts.AccessKeyId = os.Getenv("AWS_ACCESS_KEY_ID")
		opts.AccessKeyFrom = "environment"
	}
	if os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		opts.SecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
		opts.SecretKeyFrom = "environment"
	}

	files := flag.Args()

	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "You must specify at least one file to delete.")
		os.Exit(common.EXIT_USER_ERR)
	}

	return opts, files
}

// Tell the user about the program.
func printUsage() {
	message := `
apt_delete deletes a file from an S3 bucket

Usage:

apt_delete [options] key

apt_delete --config=<path to config file> \
	   [--region=<AWS region>] \
	   --bucket=<bucket to delete from> \
	   file1 ... fileN

apt_delete --help
apt_delete --version

Options:

Note that option flags may be preceded by either one or two dashes,
so -option is the same as --option.

--bucket and --key are required params. This program will get your
AWS credentials from the config file, if it can find one. Otherwise,
it will get your AWS credentials from the environment variables
"AWS_ACCESS_KEY_ID" and "AWS_SECRET_ACCESS_KEY". If it can't find your
AWS credentials, it will exit with an error message.

--bucket is the name of the S3 bucket containing the key you want to delete.

--config is the optional path to your APTrust partner config file.
  If you omit this, the program uses the config at
  ~/.aptrust_partner.conf (Mac/Linux) or %HOMEPATH%\.aptrust_partner.conf
  (Windows) if that file exists. The config file should contain
  your AWS keys, and the locations of your receiving bucket.
  For info about what should be in your config file, see
  https://wiki.aptrust.org/Partner_Tools

--help prints this help message and exits.

--region is the S3 region to connect to. This defaults to us-east-1.

--version prints version info and exits.

Following the options on the command line, list one or more keys (S3 object
names) that you want to delete. This should be a whitespace-separated list.
If keys contain whitespace, quote them.

Example:

   Delete three keys from the bucket called "my_bucket"

   apt_delete -bucket=my_bucket old_file.pdf old_image.jpg old_windbag.trump

Exit codes:

0   - Program completed successfully.
1   - Operation could not be completed due to runtime, network, or server error
3   - Operation could not be completed due to usage error (e.g. missing params)
4   - File/key does not exist. [The current underlying Amazon S3 library
      does not report these errors. Deleting a non-existent key returns success.]
100 - Printed help or version message. No other operations attempted.
`
	fmt.Println(message)
}
