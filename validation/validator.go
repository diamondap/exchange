package validation

import (
	"bufio"
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"github.com/APTrust/exchange/constants"
	"github.com/APTrust/exchange/models"
	"github.com/APTrust/exchange/platform"
	"github.com/APTrust/exchange/util"
	"github.com/APTrust/exchange/util/fileutil"
	"github.com/APTrust/exchange/util/storage"
	"github.com/satori/go.uuid"
	"hash"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

const VALIDATION_DB_SUFFIX = ".valdb"

// Validator validates a BagIt bag using a BagValidationConfig
// object, which describes the bag's requirements.
type Validator struct {
	PathToBag                  string
	BagValidationConfig        *BagValidationConfig
	PreserveExtendedAttributes bool
	summary                    *models.WorkSummary
	objIdentifier              string
	tagFilesToParse            []string
	manifests                  []string
	tagManifests               []string
	calculateMd5               bool
	calculateSha256            bool
	db                         *storage.BoltDB
}

// NewValidator creates a new Validator. Param pathToBag
// should be an absolute path to either the tarred bag (.tar file)
// or to the untarred bag (a directory). Param bagValidationConfig
// defines what we need to validate, in addition to the checksums in the
// manifests. If param preserveExtendedAttributes is true, the validator
// will preserve special data attributes used by the APTrust ingest
// process, AND will leave the .valdb validation database in place after
// it completes its work. If false, it will keep just enough data to validate
// file lists and checksums, and will delete the .valdb database when
// it's finished.
func NewValidator(pathToBag string, bagValidationConfig *BagValidationConfig, preserveExtendedAttributes bool) (*Validator, error) {
	err := validateParams(pathToBag, bagValidationConfig)
	if err != nil {
		return nil, err
	}
	calculateMd5 := util.StringListContains(bagValidationConfig.FixityAlgorithms, constants.AlgMd5)
	calculateSha256 := util.StringListContains(bagValidationConfig.FixityAlgorithms, constants.AlgSha256)
	tagFilesToParse := make([]string, 0)
	for pathToFile, filespec := range bagValidationConfig.FileSpecs {
		if filespec.ParseAsTagFile {
			tagFilesToParse = append(tagFilesToParse, pathToFile)
		}
	}
	validator := &Validator{
		PathToBag:                  pathToBag,
		BagValidationConfig:        bagValidationConfig,
		PreserveExtendedAttributes: preserveExtendedAttributes,
		summary:                    models.NewWorkSummary(),
		objIdentifier:              util.CleanBagName(path.Base(pathToBag)),
		manifests:                  make([]string, 0),
		tagManifests:               make([]string, 0),
		tagFilesToParse:            tagFilesToParse,
		calculateMd5:               calculateMd5,
		calculateSha256:            calculateSha256,
	}
	return validator, nil
}

// validateParams returns an error if there's a problem with the parameters
// pathToBag or bagValidationConfig.
func validateParams(pathToBag string, bagValidationConfig *BagValidationConfig) error {
	if !fileutil.FileExists(pathToBag) {
		return fmt.Errorf("Bag does not exist at %s", pathToBag)
	}
	if bagValidationConfig == nil {
		return fmt.Errorf("Param bagValidationConfig cannot be nil")
	}
	configErrors := bagValidationConfig.ValidateConfig()
	if len(configErrors) > 0 {
		errString := "BagValidationConfig has the following errors:"
		for _, e := range configErrors {
			errString += fmt.Sprintf("\n%s", e.Error())
		}
		return fmt.Errorf(errString)
	}
	err := bagValidationConfig.CompileFileNameRegex()
	if err != nil {
		return fmt.Errorf("Error in BagValidationConfig: %v", err)
	}
	return nil
}

// DBName returns the name of the BoltDB file where the validator keeps
// track of validation data.
func (validator *Validator) DBName() string {
	bagPath := validator.PathToBag
	if strings.HasSuffix(bagPath, ".tar") {
		bagPath = bagPath[0 : len(bagPath)-4]
	}
	if strings.HasSuffix(bagPath, string(os.PathSeparator)) {
		bagPath = bagPath[0 : len(bagPath)-1]
	}
	return fmt.Sprintf("%s%s", bagPath, VALIDATION_DB_SUFFIX)
}

// getIterator returns either a tar file iterator or a filesystem
// iterator, depending on whether we're reading a tarred bag or
// an untarred one.
func (validator *Validator) getIterator() (fileutil.ReadIterator, error) {
	if strings.HasSuffix(validator.PathToBag, ".tar") {
		return fileutil.NewTarFileIterator(validator.PathToBag)
	}
	return fileutil.NewFileSystemIterator(validator.PathToBag)
}

// Validate reads and validates the bag, and returns a ValidationResult with
// the IntellectualObject and any errors encountered during validation.
func (validator *Validator) Validate() (*models.WorkSummary, error) {
	db, err := storage.NewBoltDB(validator.DBName())
	if err != nil {
		return nil, err
	}
	validator.db = db

	validator.summary.Start()
	validator.summary.Attempted = true
	validator.summary.AttemptNumber += 1

	validator.readBag()

	if !validator.summary.HasErrors() {
		// -------------- TODO: START HERE ------------------------
		// validator.verifyManifestPresent(result)
		// validator.verifyTopLevelFolder(result)
		// validator.verifyFileSpecs(result)
		// validator.verifyTagSpecs(result)
		// validator.verifyGenericFiles(result)
	}

	validator.summary.Finish()
	return validator.summary, nil
}

// readBag reads through the contents of the bag and creates a list of
// GenericFiles. This function creates a lightweight record of the
// IntellectualObject in the db, and a for each file in the bag
// (payload files, manifests, and everything else).
func (validator *Validator) readBag() {
	// Call this for the side-effect of initializing the IntellectualObject
	// if it doesn't already exist.
	_, err := validator.getIntellectualObject()
	if err != nil {
		validator.summary.AddError("Could not init object: %v", err)
		return
	}
	validator.addFiles()
	if validator.summary.HasErrors() {
		return
	}
	validator.parseManifestsTagFilesAndMimeTypes()
}

// getIntellectualObject returns a lightweight representation of the
// IntellectualObject that this bag represents. The IntellectualObject
// will not include PremisEvents or GenericFiles. GenericFiles are
// stored separately in the db.
func (validator *Validator) getIntellectualObject() (*models.IntellectualObject, error) {
	obj, err := validator.db.GetIntellectualObject(validator.objIdentifier)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return validator.initIntellectualObject()
	}
	return obj, err
}

// initIntellectualObject creates a barebones IntellectualObject.
func (validator *Validator) initIntellectualObject() (*models.IntellectualObject, error) {
	obj := models.NewIntellectualObject()
	obj.Identifier = validator.objIdentifier
	if strings.HasSuffix(validator.PathToBag, ".tar") {
		obj.IngestTarFilePath = validator.PathToBag
	} else {
		obj.IngestUntarredPath = validator.PathToBag
	}
	err := validator.db.Save(obj.Identifier, obj)
	return obj, err
}

// addFiles adds a record for each file to our validation database.
func (validator *Validator) addFiles() {
	iterator, err := validator.getIterator()
	if err != nil {
		validator.summary.AddError("Error getting file iterator: %v", err)
		return
	}
	for {
		err := validator.addFile(iterator)
		if err != nil && (err == io.EOF || err.Error() == "EOF") {
			break // readIterator hit the end of the list
		} else if err != nil {
			validator.summary.AddError(err.Error())
		}
	}
}

// addFile adds a record for a single file to our validation database.
func (validator *Validator) addFile(readIterator fileutil.ReadIterator) error {
	reader, fileSummary, err := readIterator.Next()
	if err != nil {
		return err
	}
	if !fileSummary.IsRegularFile {
		return nil
	}
	gf := models.NewGenericFile()
	gf.Identifier = fmt.Sprintf("%s/%s", validator.objIdentifier, fileSummary.RelPath)

	// Figure out whether this is a manifest, payload file, etc.
	// This is not the same as setting the file's mime type.
	validator.setFileType(gf, fileSummary)

	// The following info is used by the APTrust ingest process,
	// but is not relevant to anyone doing validation outside
	// the APTrust organization.
	if validator.PreserveExtendedAttributes {
		_uuid := uuid.NewV4()
		gf.IntellectualObjectIdentifier = validator.objIdentifier
		gf.Size = fileSummary.Size
		gf.FileModified = fileSummary.ModTime
		gf.IngestLocalPath = fileSummary.AbsPath // will be empty if bag is tarred
		gf.IngestUUID = _uuid.String()
		gf.IngestUUIDGeneratedAt = time.Now().UTC()
		gf.IngestFileUid = fileSummary.Uid
		gf.IngestFileGid = fileSummary.Gid
	}

	// We calculate checksums in all contexts, because that's part of
	// basic bag validation. Even if checksum calculation fails (which
	// has not yet happened), we still want to keep a record of the
	// GenericFile in the validation DB for later reporting purposes.
	checksumError := validator.calculateChecksums(reader, gf)
	saveError := validator.db.Save(gf.Identifier, gf)
	if checksumError != nil {
		return checksumError
	}
	return saveError
}

// calculateChecksums calculates the checksums on the given GenericFile.
// Depending on the config options, we may calculate multiple checksums
// in a single pass. (One of the perks of golang's MultiWriter.)
func (validator *Validator) calculateChecksums(reader io.Reader, gf *models.GenericFile) error {
	hashes := make([]io.Writer, 0)
	var md5Hash hash.Hash
	var sha256Hash hash.Hash
	if validator.calculateMd5 {
		md5Hash = md5.New()
		hashes = append(hashes, md5Hash)
	}
	if validator.calculateSha256 {
		sha256Hash = sha256.New()
		hashes = append(hashes, sha256Hash)
	}
	if len(hashes) > 0 {
		multiWriter := io.MultiWriter(hashes...)
		io.Copy(multiWriter, reader)
		utcNow := time.Now().UTC()
		if md5Hash != nil {
			gf.IngestMd5 = fmt.Sprintf("%x", md5Hash.Sum(nil))
			if validator.PreserveExtendedAttributes {
				gf.IngestMd5GeneratedAt = utcNow
			}
		}
		if sha256Hash != nil {
			gf.IngestSha256 = fmt.Sprintf("%x", sha256Hash.Sum(nil))
			if validator.PreserveExtendedAttributes {
				gf.IngestSha256GeneratedAt = utcNow
			}
		}
	}
	return nil
}

// setFileType figures whether a file is a manifest, tag manifest,
// tag file or payload file. If the file is a manifest that we'll
// need to parse during the second phase of validation, this will
// add it to the list of Manifests or TagManifests.
func (validator *Validator) setFileType(gf *models.GenericFile, fileSummary *fileutil.FileSummary) {
	if strings.HasPrefix(fileSummary.RelPath, "tagmanifest-") {
		gf.IngestFileType = constants.TAG_MANIFEST
		gf.FileFormat = "text/plain"
		validator.tagManifests = append(validator.tagManifests, fileSummary.RelPath)
	} else if strings.HasPrefix(fileSummary.RelPath, "manifest-") {
		gf.IngestFileType = constants.PAYLOAD_MANIFEST
		gf.FileFormat = "text/plain"
		validator.manifests = append(validator.manifests, fileSummary.RelPath)
	} else if strings.HasPrefix(fileSummary.RelPath, "data/") {
		gf.IngestFileType = constants.PAYLOAD_FILE
	} else {
		gf.IngestFileType = constants.TAG_FILE
	}
}

//
func (validator *Validator) parseManifestsTagFilesAndMimeTypes() {
	// We have to get a new iterator here, because if we're
	// dealing with a TarFileIterator (which is likely), it's
	// forward-only. We can't rewind it.
	readIterator, err := validator.getIterator()
	if err != nil {
		validator.summary.AddError("Error getting file iterator: %v", err)
		return
	}
	for {
		reader, fileSummary, err := readIterator.Next()
		if reader != nil {
			defer reader.Close()
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			validator.summary.AddError(err.Error())
			continue
		}
		// genericFile will sometimes be nil because the iterator
		// returns directory names as well as file names
		gfIdentifier := fmt.Sprintf("%s/%s", validator.objIdentifier, fileSummary.RelPath)
		gf, err := validator.db.GetGenericFile(gfIdentifier)
		if err != nil {
			validator.summary.AddError("Error finding '%s' in validation db: %v", gfIdentifier, err)
			continue
		}
		if gf == nil {
			validator.summary.AddError("Cannot find '%s' in validation db", gfIdentifier)
			continue
		}
		validator.parseOrSetMimeType(reader, gf, fileSummary)
	}
}

// parseOrSetMimeType parses a file's contents if the file is a manifest,
// tag manifest, or parsable plain-text tag file. It the file is none of
// those, this will set its mime type (e.g. application/xml). We set mime
// type only if we're tracking extended attributes. For general validation,
// mime-type is irrelevant.
func (validator *Validator) parseOrSetMimeType(reader io.ReadCloser, gf *models.GenericFile, fileSummary *fileutil.FileSummary) {

	parseAsTagFile := util.StringListContains(validator.tagFilesToParse, fileSummary.RelPath)
	parseAsManifest := util.StringListContains(validator.manifests, fileSummary.RelPath) ||
		util.StringListContains(validator.tagManifests, fileSummary.RelPath)
	weCareAboutMimeTypes := (gf != nil && validator.PreserveExtendedAttributes)

	if parseAsTagFile {
		validator.parseTags(reader, fileSummary.RelPath)
		if weCareAboutMimeTypes {
			// We can only parse text files, so this
			// should be a plain text file.
			if strings.HasSuffix(gf.Identifier, ".txt") {
				gf.FileFormat = "text/plain"
			} else {
				gf.FileFormat = "application/binary"
			}
		}
	} else if parseAsManifest {
		// Get the checksums out of the manifest.
		validator.parseManifest(reader, fileSummary)
	} else if weCareAboutMimeTypes {
		// This is either a payload file, or some kind of tag
		// file that we don't know how to parse, so just figure
		// out its mime type for reporting and storage.
		// APTrust tracks file mime types, but we don't need this for
		// basic validation.
		validator.setMimeType(reader, gf)
	}
}

// parseTags parses the tags in a bagit-format tag file. That's a plain-text
// file with names and values separated by a colon.
func (validator *Validator) parseTags(reader io.Reader, relFilePath string) {
	obj, err := validator.getIntellectualObject()
	if err != nil {
		validator.summary.AddError("Error getting IntelObj from validation db: %v", err)
		return
	}
	if obj == nil {
		validator.summary.AddError("IntelObj '%s' is missing from validation db", validator.objIdentifier)
		return
	}
	re := regexp.MustCompile(`^(\S*\:)?(\s.*)?$`)
	scanner := bufio.NewScanner(reader)
	var tag *models.Tag
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if re.MatchString(line) {
			data := re.FindStringSubmatch(line)
			data[1] = strings.Replace(data[1], ":", "", 1)
			if data[1] != "" {
				if tag != nil && tag.Label != "" {
					obj.IngestTags = append(obj.IngestTags, tag)
				}
				tag = models.NewTag(relFilePath, data[1], strings.Trim(data[2], " "))
				validator.setIntelObjTagValue(obj, tag)
				continue
			}
			value := strings.Trim(data[2], " ")
			tag.Value = strings.Join([]string{tag.Value, value}, " ")
			validator.setIntelObjTagValue(obj, tag)
		} else {
			validator.summary.AddError("Unable to parse tag data from line: '%s'", line)
		}
	}
	if tag.Label != "" {
		obj.IngestTags = append(obj.IngestTags, tag)
	}
	if scanner.Err() != nil {
		validator.summary.AddError("Error reading tag file '%s': %v",
			relFilePath, scanner.Err().Error())
	}
	err = validator.db.Save(validator.objIdentifier, obj)
	if err != nil {
		validator.summary.AddError("Could not save IntelObj after parsing tags: %v", err)
	}
}

// Copy certain values from the aptrust-info.txt file into
// properties of the IntellectualObject.
func (validator *Validator) setIntelObjTagValue(obj *models.IntellectualObject, tag *models.Tag) {
	if tag.SourceFile == "aptrust-info.txt" {
		label := strings.ToLower(tag.Label)
		switch label {
		case "title":
			obj.Title = tag.Value
		case "access":
			obj.Access = tag.Value
		}
	} else if tag.SourceFile == "bag-info.txt" {
		label := strings.ToLower(tag.Label)
		switch label {
		case "source-organization":
			obj.Institution = tag.Value
		case "internal-sender-description":
			obj.Description = tag.Value
		case "internal-sender-identifier":
			obj.AltIdentifier = tag.Value
		}
	}
}

// Parse the checksums in a manifest.
func (validator *Validator) parseManifest(reader io.Reader, fileSummary *fileutil.FileSummary) {
	alg := ""
	if strings.Contains(fileSummary.RelPath, constants.AlgSha256) {
		alg = constants.AlgSha256
	} else if strings.Contains(fileSummary.RelPath, constants.AlgMd5) {
		alg = constants.AlgMd5
	} else {
		fmt.Fprintln(os.Stderr, "Not verifying checksums in", fileSummary.RelPath,
			"- unsupported algorithm. Will still verify any md5 or sha256 checksums.")
		return
	}
	re := regexp.MustCompile(`^(\S*)\s*(.*)`)
	scanner := bufio.NewScanner(reader)
	lineNum := 1
	for scanner.Scan() {
		updateGenericFile := false
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if re.MatchString(line) {
			data := re.FindStringSubmatch(line)
			digest := data[1]
			filePath := data[2]

			gfIdentifier := fmt.Sprintf("%s/%s", validator.objIdentifier, fileSummary.RelPath)
			genericFile, err := validator.db.GetGenericFile(filePath)
			if err != nil {
				validator.summary.AddError("Error finding generic file '%s' in db: %v", gfIdentifier)
			}
			if genericFile == nil {
				validator.summary.AddError(
					"File '%s' in manifest '%s' is missing from bag",
					filePath, fileSummary.RelPath)
				continue
			}

			// If we got a digest from this line of the manifest,
			// set it on the GenericFile and save the record back
			// to the database.
			if alg == constants.AlgMd5 {
				genericFile.IngestManifestMd5 = digest
				updateGenericFile = true
			} else if alg == constants.AlgSha256 {
				genericFile.IngestManifestSha256 = digest
				updateGenericFile = true
			}
			if updateGenericFile {
				err = validator.db.Save(gfIdentifier, genericFile)
				if err != nil {
					validator.summary.AddError("Error saving generic file '%s' to db: %v", gfIdentifier, err)
				}
			}
		} else {
			validator.summary.AddError(fmt.Sprintf(
				"Unable to parse data from line %d of manifest %s: %s",
				lineNum, fileSummary.RelPath, line))
		}
		lineNum += 1
	}
}

// setMimeType attempts to set the FileFormat attribute to the correct
// mime type (e.g. image/jpeg). We do not run this function unless we're
// PreserveExtendedAttributes is on. This function does nothing on Windows.
// It only sets a meaningful file type on *nix platforms that have access
// to the mime magic database.
func (validator *Validator) setMimeType(reader io.Reader, gf *models.GenericFile) {
	// on err, defaults to application/binary
	bufLen := 128
	if gf.Size < int64(bufLen) {
		bufLen = int(gf.Size - 1)
		if bufLen < 1 {
			// We actually do permit zero-length files, and we can
			// save them in S3. These files can be necessary in
			// certain cases, such as __init__.py files for Python,
			// PHP templates whose presence is required, ".keep" files, etc.
			gf.FileFormat = "text/empty"
			return
		}
	}
	buf := make([]byte, bufLen)
	_, err := reader.Read(buf)
	if err != nil {
		validator.summary.AddError(err.Error())
	}
	gf.FileFormat, err = platform.GuessMimeTypeByBuffer(buf)
	if err != nil {
		validator.summary.AddError(err.Error())
	}
}
