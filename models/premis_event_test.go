package models_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/APTrust/exchange/constants"
	"github.com/APTrust/exchange/models"
	"github.com/APTrust/exchange/util/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const digest = "12345678901234567890123456789012"
const md5_digest = "md5:12345678901234567890123456789012"
const sha256_digest = "sha256:12345678901234567890123456789012"

func TestEventTypeValid(t *testing.T) {
	for _, eventType := range constants.EventTypes {
		premisEvent := &models.PremisEvent{
			EventType: eventType,
		}
		if premisEvent.EventTypeValid() == false {
			t.Errorf("EventType '%s' should be valid", eventType)
		}
	}
	premisEvent := &models.PremisEvent{
		EventType: "pub_crawl",
	}
	if premisEvent.EventTypeValid() == true {
		t.Errorf("EventType 'pub_crawl' should not be valid")
	}
}

func TestNewEventObjectCreation(t *testing.T) {
	event := models.NewEventObjectCreation()
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "creation", event.EventType)
	assert.False(t, event.DateTime.IsZero())
	assert.Equal(t, "Object created.", event.Detail)
	assert.Equal(t, "Success", event.Outcome)
	assert.Equal(t, "Intellectual object created.", event.OutcomeDetail)
	assert.Equal(t, "APTrust Exchange ingest services", event.Object)
	assert.Equal(t, "https://github.com/APTrust/exchange", event.Agent)
	assert.True(t, strings.HasPrefix(event.OutcomeInformation, "Object created, files stored and replicated"))
}

func TestNewEventObjectIngest(t *testing.T) {
	// Test with required params missing
	_, err := models.NewEventObjectIngest(0)
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}

	event, err := models.NewEventObjectIngest(300)
	if err != nil {
		t.Errorf("Error creating PremisEvent: %v", err)
		return
	}
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "ingestion", event.EventType)
	assert.False(t, event.DateTime.IsZero())
	assert.Equal(t, "Copied all files to perservation bucket", event.Detail)
	assert.Equal(t, "Success", event.Outcome)
	assert.Equal(t, "300 files copied", event.OutcomeDetail)
	assert.Equal(t, "AWS Go SDK S3 client", event.Object)
	assert.Equal(t, "https://github.com/aws/aws-sdk-go", event.Agent)
	assert.Equal(t, "Multipart put using md5 checksum", event.OutcomeInformation)
}

func TestNewEventObjectIdentifierAssignment(t *testing.T) {
	// Test with required params missing
	_, err := models.NewEventObjectIdentifierAssignment("")
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}

	event, err := models.NewEventObjectIdentifierAssignment("test.edu/object001")
	if err != nil {
		t.Errorf("Error creating PremisEvent: %v", err)
		return
	}
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "identifier assignment", event.EventType)
	assert.False(t, event.DateTime.IsZero())
	assert.Equal(t, "Assigned bag identifier", event.Detail)
	assert.Equal(t, "Success", event.Outcome)
	assert.Equal(t, "test.edu/object001", event.OutcomeDetail)
	assert.Equal(t, "APTrust exchange", event.Object)
	assert.Equal(t, "https://github.com/APTrust/exchange", event.Agent)
	assert.Equal(t, "Institution domain + tar file name", event.OutcomeInformation)
}

func TestNewEventObjectRights(t *testing.T) {
	// Test with required params missing
	_, err := models.NewEventObjectRights("")
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}

	event, err := models.NewEventObjectRights("institution")
	if err != nil {
		t.Errorf("Error creating PremisEvent: %v", err)
		return
	}
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "access assignment", event.EventType)
	assert.False(t, event.DateTime.IsZero())
	assert.Equal(t, "Assigned bag access rights", event.Detail)
	assert.Equal(t, "Success", event.Outcome)
	assert.Equal(t, "institution", event.OutcomeDetail)
	assert.Equal(t, "APTrust exchange", event.Object)
	assert.Equal(t, "https://github.com/APTrust/exchange", event.Agent)
	assert.Equal(t, "Set access to institution", event.OutcomeInformation)
}

func TestNewEventGenericFileIngest(t *testing.T) {
	// Test with required params missing
	_, err := models.NewEventGenericFileIngest(time.Time{}, digest, "")
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param storedAt"))
	}
	_, err = models.NewEventGenericFileIngest(testutil.TEST_TIMESTAMP, "", "")
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param md5Digest"))
	}
	_, err = models.NewEventGenericFileIngest(testutil.TEST_TIMESTAMP, digest, "")
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param _uuid"))
	}

	_uuid := "c5c34a8c-4c70-4140-8442-594f4fcaf4b9"

	event, err := models.NewEventGenericFileIngest(testutil.TEST_TIMESTAMP, digest, _uuid)
	if err != nil {
		t.Errorf("Error creating PremisEvent: %v", err)
		return
	}
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "ingestion", event.EventType)
	assert.Equal(t, testutil.TEST_TIMESTAMP, event.DateTime)
	assert.Equal(t, "Completed copy to S3 (c5c34a8c-4c70-4140-8442-594f4fcaf4b9)", event.Detail)
	assert.Equal(t, "Success", event.Outcome)
	assert.Equal(t, md5_digest, event.OutcomeDetail)
	assert.Equal(t, "exchange + AWS Go SDK S3 client", event.Object)
	assert.Equal(t, "https://github.com/aws/aws-sdk-go", event.Agent)
	assert.Equal(t, "Put using md5 checksum", event.OutcomeInformation)
}

func TestNewEventGenericFileFixityCheck(t *testing.T) {
	// Test with required params missing
	_, err := models.NewEventGenericFileFixityCheck(time.Time{}, constants.AlgMd5, digest, true)
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}
	_, err = models.NewEventGenericFileFixityCheck(testutil.TEST_TIMESTAMP, "", digest, true)
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}
	_, err = models.NewEventGenericFileFixityCheck(testutil.TEST_TIMESTAMP, constants.AlgMd5, "", true)
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}

	event, err := models.NewEventGenericFileFixityCheck(testutil.TEST_TIMESTAMP, constants.AlgMd5,
		digest, true)
	if err != nil {
		t.Errorf("Error creating PremisEvent: %v", err)
		return
	}
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "fixity check", event.EventType)
	assert.Equal(t, testutil.TEST_TIMESTAMP, event.DateTime)
	assert.Equal(t, "Fixity check against registered hash", event.Detail)
	assert.Equal(t, "Success", event.Outcome)
	assert.Equal(t, md5_digest, event.OutcomeDetail)
	assert.Equal(t, "Go language crypto/md5", event.Object)
	assert.Equal(t, "http://golang.org/pkg/crypto/md5/", event.Agent)
	assert.Equal(t, "Fixity matches", event.OutcomeInformation)

	event, err = models.NewEventGenericFileFixityCheck(testutil.TEST_TIMESTAMP, constants.AlgSha256,
		digest, false)
	if err != nil {
		t.Errorf("Error creating PremisEvent: %v", err)
		return
	}
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "fixity check", event.EventType)
	assert.Equal(t, testutil.TEST_TIMESTAMP, event.DateTime)
	assert.Equal(t, "Fixity check against registered hash", event.Detail)
	assert.Equal(t, "Failed", event.Outcome)
	assert.Equal(t, sha256_digest, event.OutcomeDetail)
	assert.Equal(t, "Go language crypto/sha256", event.Object)
	assert.Equal(t, "http://golang.org/pkg/crypto/sha256/", event.Agent)
	assert.Equal(t, "Fixity did not match", event.OutcomeInformation)
}

func TestNewEventGenericFileDigestCalculation(t *testing.T) {
	// Test with required params missing
	_, err := models.NewEventGenericFileDigestCalculation(time.Time{}, constants.AlgMd5, digest)
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}
	_, err = models.NewEventGenericFileDigestCalculation(testutil.TEST_TIMESTAMP, "", digest)
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}
	_, err = models.NewEventGenericFileDigestCalculation(testutil.TEST_TIMESTAMP, constants.AlgMd5, "")
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}

	event, err := models.NewEventGenericFileDigestCalculation(testutil.TEST_TIMESTAMP,
		constants.AlgMd5, digest)
	if err != nil {
		t.Errorf("Error creating PremisEvent: %v", err)
		return
	}
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "message digest calculation", event.EventType)
	assert.Equal(t, testutil.TEST_TIMESTAMP, event.DateTime)
	assert.Equal(t, "Calculated fixity value", event.Detail)
	assert.Equal(t, "Success", event.Outcome)
	assert.Equal(t, md5_digest, event.OutcomeDetail)
	assert.Equal(t, "Go language crypto/md5", event.Object)
	assert.Equal(t, "http://golang.org/pkg/crypto/md5/", event.Agent)
	assert.Equal(t, "Calculated fixity value", event.OutcomeInformation)

	event, err = models.NewEventGenericFileDigestCalculation(testutil.TEST_TIMESTAMP, constants.AlgSha256, digest)
	if err != nil {
		t.Errorf("Error creating PremisEvent: %v", err)
		return
	}
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "message digest calculation", event.EventType)
	assert.Equal(t, testutil.TEST_TIMESTAMP, event.DateTime)
	assert.Equal(t, "Calculated fixity value", event.Detail)
	assert.Equal(t, "Success", event.Outcome)
	assert.Equal(t, sha256_digest, event.OutcomeDetail)
	assert.Equal(t, "Go language crypto/sha256", event.Object)
	assert.Equal(t, "http://golang.org/pkg/crypto/sha256/", event.Agent)
	assert.Equal(t, "Calculated fixity value", event.OutcomeInformation)
}

func TestNewEventGenericFileIdentifierAssignment(t *testing.T) {
	// Test with required params missing
	_, err := models.NewEventGenericFileIdentifierAssignment(time.Time{}, constants.AlgMd5, "abc/123")
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}
	_, err = models.NewEventGenericFileIdentifierAssignment(testutil.TEST_TIMESTAMP, "", "abc/123")
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}
	_, err = models.NewEventGenericFileIdentifierAssignment(testutil.TEST_TIMESTAMP, constants.AlgMd5, "")
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}

	event, err := models.NewEventGenericFileIdentifierAssignment(testutil.TEST_TIMESTAMP, constants.IdTypeBagAndPath, "blah.edu/blah/blah.txt")
	if err != nil {
		t.Errorf("Error creating PremisEvent: %v", err)
		return
	}
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "identifier assignment", event.EventType)
	assert.Equal(t, testutil.TEST_TIMESTAMP, event.DateTime)
	assert.Equal(t, "Assigned new institution.bag/path identifier", event.Detail)
	assert.Equal(t, "Success", event.Outcome)
	assert.Equal(t, "blah.edu/blah/blah.txt", event.OutcomeDetail)
	assert.Equal(t, "APTrust exchange/ingest processor", event.Object)
	assert.Equal(t, "https://github.com/APTrust/exchange", event.Agent)
	assert.Equal(t, "Assigned bag/filepath identifier", event.OutcomeInformation)

	event, err = models.NewEventGenericFileIdentifierAssignment(testutil.TEST_TIMESTAMP, constants.IdTypeStorageURL, "https://example.com/000-000-999")
	if err != nil {
		t.Errorf("Error creating PremisEvent: %v", err)
		return
	}
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "identifier assignment", event.EventType)
	assert.Equal(t, testutil.TEST_TIMESTAMP, event.DateTime)
	assert.True(t, strings.HasPrefix(event.Detail, "Assigned new storage URL identifier"))
	assert.Equal(t, "Success", event.Outcome)
	assert.Equal(t, "https://example.com/000-000-999", event.OutcomeDetail)
	assert.Equal(t, "Go uuid library + AWS Go SDK S3 library", event.Object)
	assert.Equal(t, "http://github.com/google/uuid", event.Agent)
	assert.Equal(t, "Assigned url identifier", event.OutcomeInformation)

}

func TestNewEventGenericFileReplication(t *testing.T) {
	// Test with required params missing
	_, err := models.NewEventGenericFileReplication(time.Time{}, "https://example.com/123456789")
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}
	_, err = models.NewEventGenericFileReplication(testutil.TEST_TIMESTAMP, "")
	assert.NotNil(t, err)
	if err != nil {
		assert.True(t, strings.HasPrefix(err.Error(), "Param"))
	}

	event, err := models.NewEventGenericFileReplication(testutil.TEST_TIMESTAMP, "https://example.com/123456789")
	if err != nil {
		t.Errorf("Error creating PremisEvent: %v", err)
		return
	}
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "replication", event.EventType)
	assert.Equal(t, testutil.TEST_TIMESTAMP, event.DateTime)
	assert.Equal(t, "Copied to replication storage and assigned replication URL identifier", event.Detail)
	assert.Equal(t, "Success", event.Outcome)
	assert.Equal(t, "https://example.com/123456789", event.OutcomeDetail)
	assert.Equal(t, "Go uuid library + AWS Go SDK S3 library", event.Object)
	assert.Equal(t, "http://github.com/google/uuid", event.Agent)
	assert.Equal(t, "Replicated to secondary storage", event.OutcomeInformation)
}

func TestNewEventFileDeletion(t *testing.T) {
	fileUUID := uuid.New().String()
	utcNow := time.Now().UTC()
	event := models.NewEventFileDeletion(fileUUID, "user@example.com", "admin@example.com", "", utcNow)
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "deletion", event.EventType)
	assert.Equal(t, utcNow, event.DateTime)
	assert.Equal(t, fmt.Sprintf("File %s deleted from long-term storage.", fileUUID), event.Detail)
	assert.Equal(t, "Success", event.Outcome)
	assert.Equal(t, "File deleted at the request of user@example.com. Institutional approver: admin@example.com.",
		event.OutcomeInformation)
	assert.Equal(t, "APTrust Exchange apt_delete service", event.Object)
	assert.Equal(t, "https://github.com/APTrust/exchange", event.Agent)
	assert.Equal(t, "user@example.com", event.OutcomeDetail)

	event = models.NewEventFileDeletion(fileUUID, "user@example.com",
		"admin@example.com", "someone@aptrust.org", utcNow)
	assert.Len(t, event.Identifier, 36)
	assert.Equal(t, "deletion", event.EventType)
	assert.Equal(t, utcNow, event.DateTime)
	assert.Equal(t, fmt.Sprintf("File %s deleted from long-term storage.", fileUUID), event.Detail)
	assert.Equal(t, "Success", event.Outcome)
	assert.Equal(t,
		"File deleted at the request of user@example.com. Institutional approver: admin@example.com. APTrust approver: someone@aptrust.org.",
		event.OutcomeInformation)
	assert.Equal(t, "APTrust Exchange apt_delete service", event.Object)
	assert.Equal(t, "https://github.com/APTrust/exchange", event.Agent)
	assert.Equal(t, "user@example.com", event.OutcomeDetail)
}

func TestPremisEventMergeAttributes(t *testing.T) {
	event1 := testutil.MakePremisEvent()
	event2 := testutil.MakePremisEvent()

	err := event1.MergeAttributes(event2)
	require.Nil(t, err)
	assert.Equal(t, event1.Id, event2.Id)
	assert.Equal(t, event1.CreatedAt, event2.CreatedAt)
	assert.Equal(t, event1.UpdatedAt, event2.UpdatedAt)

	err = event1.MergeAttributes(nil)
	assert.NotNil(t, err)
}

func TestIsUrlAssignment(t *testing.T) {
	event := testutil.MakePremisEvent()
	event.EventType = constants.EventIdentifierAssignment
	event.Detail = "Assigned new storage URL blah blah blah THE INTERWEBZ!"
	assert.True(t, event.IsUrlAssignment())
	event.Detail = "What are you doing with that vat of sulfuric acid, Boris?"
	assert.False(t, event.IsUrlAssignment())
	event.EventType = constants.EventIngestion
	event.Detail = "Assigned new storage URL blah blah blah THE INTERWEBZ!"
	assert.False(t, event.IsUrlAssignment())
}

func TestPremisEventClone(t *testing.T) {
	event := models.NewEventObjectCreation()
	event.Id = 9999

	clone := event.Clone()
	assert.Equal(t, clone.Id, event.Id)
	assert.Equal(t, clone.Identifier, event.Identifier)
	assert.Equal(t, clone.EventType, event.EventType)
	assert.Equal(t, clone.DateTime, event.DateTime)
	assert.Equal(t, clone.Detail, event.Detail)
	assert.Equal(t, clone.Outcome, event.Outcome)
	assert.Equal(t, clone.OutcomeDetail, event.OutcomeDetail)
	assert.Equal(t, clone.Object, event.Object)
	assert.Equal(t, clone.Agent, event.Agent)
	assert.Equal(t, clone.OutcomeInformation, event.OutcomeInformation)
	assert.Equal(t, clone.IntellectualObjectId, event.IntellectualObjectId)
	assert.Equal(t, clone.IntellectualObjectIdentifier, event.IntellectualObjectIdentifier)
	assert.Equal(t, clone.GenericFileId, event.GenericFileId)
	assert.Equal(t, clone.GenericFileIdentifier, event.GenericFileIdentifier)
	assert.Equal(t, clone.CreatedAt, event.CreatedAt)
	assert.Equal(t, clone.UpdatedAt, event.UpdatedAt)
}
