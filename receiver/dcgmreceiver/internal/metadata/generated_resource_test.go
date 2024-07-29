// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResourceBuilder(t *testing.T) {
	for _, test := range []string{"default", "all_set", "none_set"} {
		t.Run(test, func(t *testing.T) {
			cfg := loadResourceAttributesConfig(t, test)
			rb := NewResourceBuilder(cfg)
			rb.SetGpuModel("gpu.model-val")
			rb.SetGpuNumber("gpu.number-val")
			rb.SetGpuUUID("gpu.uuid-val")

			res := rb.Emit()
			assert.Equal(t, 0, rb.Emit().Attributes().Len()) // Second call should return empty Resource

			switch test {
			case "default":
				assert.Equal(t, 3, res.Attributes().Len())
			case "all_set":
				assert.Equal(t, 3, res.Attributes().Len())
			case "none_set":
				assert.Equal(t, 0, res.Attributes().Len())
				return
			default:
				assert.Failf(t, "unexpected test case: %s", test)
			}

			val, ok := res.Attributes().Get("gpu.model")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "gpu.model-val", val.Str())
			}
			val, ok = res.Attributes().Get("gpu.number")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "gpu.number-val", val.Str())
			}
			val, ok = res.Attributes().Get("gpu.uuid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "gpu.uuid-val", val.Str())
			}
		})
	}
}