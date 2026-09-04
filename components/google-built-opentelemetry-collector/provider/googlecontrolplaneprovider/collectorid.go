package googlecontrolplaneprovider

import (
	"fmt"
	"os"

	"github.com/google/uuid"
)

var CollectorUUIDNamespace = "5AFFCE1A-3D0A-4220-8EA1-7D64A842BEB4"

var CollectorID string
var CollectorName string

type hostnameFunc func() (string, error)

var getHostname hostnameFunc = os.Hostname

func GenerateCollectorID() error {
	// If there is already a CollectorID, no need to generate one.
	if CollectorID != "" {
		return nil
	}

	// Check COLLECTOR_NAME environment variable. If there's a value,
	// set CollectorName global to that.
	if envName := os.Getenv("COLLECTOR_NAME"); envName != "" {
		CollectorName = envName
	} else {
		// Otherwise, try to detect a hostname from the environment and set the
		// CollectorName to that.
		var err error
		CollectorName, err = getHostname()
		if err != nil {
			return err
		}
	}

	// Set CollectorID to a UUID v5 hash using CollectorUUIDNamespace and
	// the CollectorName.
	namespace, err := uuid.Parse(CollectorUUIDNamespace)
	if err != nil {
		panic(fmt.Sprintf("Should be an impossible code state. Failed to parse Collector UUID Namespace: %v", err))
	}

	CollectorID = uuid.NewSHA1(namespace, []byte(CollectorName)).String()

	return nil
}
