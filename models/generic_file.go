package models

import (
	"fmt"
	"encoding/json"
	"strings"
	"time"
)

/*
GenericFile contains information about a file that makes up
part (or all) of an IntellectualObject.

IntellectualObject is the object to which the file belongs.

Format is typically a mime-type, such as "application/xml",
that describes the file format.

URI describes the location of the object (in APTrust?).

Size is the size of the object, in bytes.

FileCreated is the date and time at which the file was created
by the depositor.

FileModified is the data and time at which the object was last
modified (in APTrust, or at the institution that owns it?).

CreatedAt and UpdatedAt are Rails timestamps describing when
this GenericFile records was created and last updated.

FileCreated and FileModified should be ISO8601 DateTime strings,
such as:
1994-11-05T08:15:30-05:00     (Local Time)
1994-11-05T08:15:30Z          (UTC)
*/
type GenericFile struct {
	// Pharos fields.

	// The Rails/Database id for this generic file.
	// If the Id is non-zero, it's been recorded in Pharos.
	Id                           int            `json:"id"`

	// The human-readable identifier for this file. It consists of
	// the object name, followed by a slash, followed by the path
	// of the file within the bag. E.g. "virginia.edu/bag001/data/file1.pdf"
	Identifier                   string         `json:"identifier"`

	// The id of the IntellectualObject to which this file belongs.
	IntellectualObjectId         int            `json:"intellectual_object_id"`

	// The identifier of the intellectual object to which this file belongs.
	IntellectualObjectIdentifier string         `json:"intellectual_object_identifier"`

	// The file's mime type. E.g. "application/xml"
	FileFormat                   string         `json:"file_format"`

	// The location of this file in our primary s3 long-term storage bucket.
	URI                          string         `json:"uri"`

	// The size of the file, in bytes.
	Size                         int64          `json:"size"`

	// The date this file was created by the depositor. This date comes from
	// the file record in the tarred bag.
	FileCreated                  time.Time      `json:"file_created"`

	// The date this file was last modified by the depository. This date comes
	// from the file record in the tarred bag.
	FileModified                 time.Time      `json:"file_modified"`

	// A timestamp indicating when this GenericFile record was created in
	// our repository.
	CreatedAt                    time.Time      `json:"created_at"`

	// A timestamp indicating when this GenericFile record was last updated in
	// our repository.
	UpdatedAt                    time.Time      `json:"updated_at"`

	// A list of checksums for this file.
	Checksums                    []*Checksum    `json:"checksums"`

	// A list of PREMIS events for this file.
	PremisEvents                 []*PremisEvent `json:"premis_events"`


	// ----------------------------------------------------
	// The fields below are for internal housekeeping.
	// We don't send this data to Pharos.
	// ----------------------------------------------------


	// IngestFileType can be one of the types defined in constants.
	// PAYLOAD_FILE, PAYLOAD_MANIFEST, TAG_MANIFEST, TAG_FILE
	IngestFileType               string         `json:"ingest_file_type"`

	// IngestLocalPath is the absolute path to this file on local disk.
	// It may be empty if we're working with a tar file.
	IngestLocalPath              string         `json:"ingest_local_path"`

	// IngestManifestMd5 is the md5 checksum of this file, as reported
	// in the bag's manifest-md5.txt file. This may be empty if there
	// was no md5 checksum file, or if this generic file wasn't listed
	// in the md5 manifest.
	IngestManifestMd5            string         `json:"ingest_manifest_md5"`

	// The md5 checksum we calculated at ingest from the actual file.
	IngestMd5                    string         `json:"ingest_md5"`

	// DateTime we calculated the md5 digest from local file.
	IngestMd5GeneratedAt         time.Time      `json:"ingest_md5_generated_at"`

	// DateTime we verified that our md5 checksum matches what's in the manifest.
	IngestMd5VerifiedAt          time.Time      `json:"ingest_md5_verified_at"`

	// The sha256 checksum for this file, as reported in the payload manifest.
	// This may be empty if the bag had no sha256 manifest, or if this file
	// was not listed in the manifest.
	IngestManifestSha256         string         `json:"ingest_manifest_sha256"`

	// The sha256 checksum we calculated when we read the actual file.
	IngestSha256                 string         `json:"ingest_sha_256"`

	// Timestamp of when we calculated the sha256 checksum.
	IngestSha256GeneratedAt      time.Time      `json:"ingest_sha_256_generated_at"`

	// Timestamp of when we verified that the sha256 checksum we calculated
	// matches what's in the manifest.
	IngestSha256VerifiedAt       time.Time      `json:"ingest_sha_256_verified_at"`

	// The UUID assigned to this file. This will be its S3 key when we store it.
	IngestUUID                   string         `json:"ingest_uuid"`

	// Timestamp of when we generated the UUID for this file. Needed to create
	// the identifier assignment PREMIS event.
	IngestUUIDGeneratedAt        time.Time      `json:"ingest_uuid_generated_at"`

	// Where this file is stored in S3.
	IngestStorageURL             string         `json:"ingest_storage_url"`

	// Timestamp indicating when this file was stored in S3.
	IngestStoredAt               time.Time      `json:"ingest_stored_at"`

	// Where this file is stored in Glacier.
	IngestReplicationURL         string         `json:"ingest_replication_url"`

	// Timestamp indicating when this file was stored in Glacier.
	IngestReplicatedAt           time.Time      `json:"ingest_replicated_at"`

	// If true, a previous version of this same file exists in S3/Glacier.
	IngestPreviousVersionExists  bool           `json:"ingest_previous_version_exists"`

	// If true, this file needs to be saved to S3.
	IngestNeedsSave              bool           `json:"ingest_needs_save"`

	// Error that occurred during ingest. If empty, there was no error.
	IngestErrorMessage           string         `json:"ingesterror_message"`

	// File User Id (unreliable)
	IngestFileUid                int            `json:"ingest_file_uid"`

	// File Group Id (unreliable)
	IngestFileGid                int            `json:"ingest_file_gid"`

	// File User Name (unreliable)
	IngestFileUname              string         `json:"ingest_file_uname"`

	// File Group Name (unreliable)
	IngestFileGname              string         `json:"ingest_file_gname"`

	// File Mode/Permissions (unreliable)
	IngestFileMode               int64          `json:"ingest_file_mode"`
}

func NewGenericFile() (*GenericFile) {
	return &GenericFile{
		Checksums: make([]*Checksum, 0),
		PremisEvents: make([]*PremisEvent, 0),
		IngestPreviousVersionExists: false,
		IngestNeedsSave: true,
	}
}


// Serializes a version of GenericFile that Fluctus will accept as post/put input.
// Note that we don't serialize the id or any of our internal housekeeping info.
func (gf *GenericFile) SerializeForPharos() ([]byte, error) {
	// We have to create a temporary structure to prevent json.Marshal
	// from serializing Size (int64) with scientific notation.
	// Without this step, Size will be serialized as something like
	// 2.706525e+06, which is not valid JSON.
	temp := struct{
		Identifier                   string         `json:"identifier"`
		IntellectualObjectId         int            `json:"intellectual_object_id"`
		IntellectualObjectIdentifier string         `json:"intellectual_object_identifier"`
		FileFormat                   string         `json:"file_format"`
		URI                          string         `json:"uri"`
		Size                         int64          `json:"size"`
		FileCreated                  time.Time      `json:"file_created"`
		FileModified                 time.Time      `json:"file_modified"`
		CreatedAt                    time.Time      `json:"created_at"`
		UpdatedAt                    time.Time      `json:"updated_at"`
		Checksums                    []*Checksum    `json:"checksums"`
	} {
		Identifier:                     gf.Identifier,
		IntellectualObjectId:           gf.IntellectualObjectId,
		IntellectualObjectIdentifier:   gf.IntellectualObjectIdentifier,
		FileFormat:                     gf.FileFormat,
		URI:                            gf.URI,
		Size:                           gf.Size,
		FileCreated:                    gf.FileCreated,
		FileModified:                   gf.FileModified,
		Checksums:                      gf.Checksums,
	}
	return json.Marshal(temp)
}

// Returns the original path of the file within the original bag.
// This is just the identifier minus the institution id and bag name.
// For example, if the identifier is "uc.edu/cin.675812/data/object.properties",
// this returns "data/object.properties"
func (gf *GenericFile) OriginalPath() (string, error) {
	parts := strings.SplitN(gf.Identifier, "/", 3)
	if len(parts) < 3 {
		return "", fmt.Errorf("GenericFile identifier '%s' is not valid", gf.Identifier)
	}
	return parts[2], nil
}

// Returns the name of the institution that owns this file.
func (gf *GenericFile) InstitutionIdentifier() (string, error) {
	parts := strings.Split(gf.Identifier, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("GenericFile identifier '%s' is not valid", gf.Identifier)
	}
	return parts[0], nil
}

// Returns the checksum digest for the given algorithm for this file.
func (gf *GenericFile) GetChecksum(algorithm string) (*Checksum) {
	for _, cs := range gf.Checksums {
		if cs != nil && cs.Algorithm == algorithm {
			return cs
		}
	}
	return nil
}

// Returns events of the specified type
func (gf *GenericFile) FindEventsByType(eventType string) ([]PremisEvent) {
	events := make([]PremisEvent, 0)
	for _, event := range gf.PremisEvents {
		if event != nil && event.EventType == eventType {
			events = append(events, *event)
		}
	}
	return events
}

// Returns the name of this file in the preservation storage bucket
// (that should be a UUID), or an error if the GenericFile does not
// have a valid preservation storage URL.
func (gf *GenericFile) PreservationStorageFileName() (string, error) {
	if strings.Index(gf.URI, "/") < 0 {
		return "", fmt.Errorf("Cannot get preservation storage file name because GenericFile has an invalid URI")
	}
	parts := strings.Split(gf.URI, "/")
	return parts[len(parts) - 1], nil
}
