package googlecontrolplaneprovider

import (
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetGlobals() {
	CollectorID = ""
	CollectorName = ""
	getHostname = os.Hostname
}

func TestGenerateCollectorID_AlreadySet(t *testing.T) {
	resetGlobals()
	defer resetGlobals()

	CollectorID = "existing-id"
	CollectorName = "existing-name"

	err := GenerateCollectorID()
	require.NoError(t, err)
	assert.Equal(t, "existing-id", CollectorID)
	assert.Equal(t, "existing-name", CollectorName)
}

func TestGenerateCollectorID_FromEnv(t *testing.T) {
	resetGlobals()
	defer resetGlobals()

	t.Setenv("COLLECTOR_NAME", "custom-collector")

	err := GenerateCollectorID()
	require.NoError(t, err)
	assert.Equal(t, "custom-collector", CollectorName)

	ns := uuid.MustParse(CollectorUUIDNamespace)
	expectedID := uuid.NewSHA1(ns, []byte("custom-collector")).String()
	assert.Equal(t, expectedID, CollectorID)
}

func TestGenerateCollectorID_DefaultHostname(t *testing.T) {
	resetGlobals()
	defer resetGlobals()

	pretendHostname := "testhost"
	getHostname = func() (string, error) {
		return pretendHostname, nil
	}
	t.Setenv("COLLECTOR_NAME", "")

	err := GenerateCollectorID()
	require.NoError(t, err)
	assert.Equal(t, pretendHostname, CollectorName)

	ns := uuid.MustParse(CollectorUUIDNamespace)
	expectedID := uuid.NewSHA1(ns, []byte(pretendHostname)).String()
	assert.Equal(t, expectedID, CollectorID)
}

func TestGenerateCollectorID_GetHostnameError(t *testing.T) {
	resetGlobals()
	defer resetGlobals()

	expectedErr := errors.New("failed to get hostname")
	getHostname = func() (string, error) {
		return "", expectedErr
	}
	t.Setenv("COLLECTOR_NAME", "")

	err := GenerateCollectorID()
	require.ErrorIs(t, err, expectedErr)
	assert.Empty(t, CollectorID)
	assert.Empty(t, CollectorName)
}

