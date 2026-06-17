package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// SegmentConfig.UnmarshalYAML (types.go) is a custom unmarshaller giving status-
// line segments dual-form polymorphism: a segment may be a bare string ("- cost")
// OR a full struct ("- builtin: cost" / "- custom: {command: ...}"). It had zero
// unit coverage. This is a silent-fail-open surface: a regression in the bare-
// string-then-struct-fallback logic would parse valid configs wrong, and ARCH-1
// fail-open would hide it (no status-line output, exit 0). These tests pin both
// forms and their coexistence.

func TestSegmentConfig_UnmarshalYAML_BareString(t *testing.T) {
	var slc StatusLineConfig
	require.NoError(t, yaml.Unmarshal([]byte("segments:\n  - cost\n  - news\n"), &slc))

	require.Len(t, slc.Segments, 2)
	assert.Equal(t, "cost", slc.Segments[0].Builtin, "a bare string segment must populate Builtin")
	assert.Nil(t, slc.Segments[0].Custom, "a bare string segment must not create a Custom struct")
	assert.Equal(t, "news", slc.Segments[1].Builtin)
}

func TestSegmentConfig_UnmarshalYAML_Struct(t *testing.T) {
	y := "segments:\n" +
		"  - builtin: cost\n" +
		"  - custom:\n" +
		"      command: ./my-seg.sh\n" +
		"      label: MySeg\n"
	var slc StatusLineConfig
	require.NoError(t, yaml.Unmarshal([]byte(y), &slc))

	require.Len(t, slc.Segments, 2)
	assert.Equal(t, "cost", slc.Segments[0].Builtin, "an explicit {builtin:} struct must populate Builtin")
	require.NotNil(t, slc.Segments[1].Custom, "a {custom:} struct must deserialize into Custom")
	assert.Equal(t, "./my-seg.sh", slc.Segments[1].Custom.Command)
	assert.Equal(t, "MySeg", slc.Segments[1].Custom.Label)
	assert.Empty(t, slc.Segments[1].Builtin, "a custom segment must not also set Builtin")
}

func TestSegmentConfig_UnmarshalYAML_Mixed(t *testing.T) {
	// Bare string and full struct coexisting in one array — the form the bug
	// class most likely breaks, since it exercises both the string branch and
	// the struct fallback within a single sequence decode.
	y := "segments:\n" +
		"  - cost\n" +
		"  - custom:\n" +
		"      command: ./x.sh\n"
	var slc StatusLineConfig
	require.NoError(t, yaml.Unmarshal([]byte(y), &slc))

	require.Len(t, slc.Segments, 2)
	assert.Equal(t, "cost", slc.Segments[0].Builtin)
	assert.Nil(t, slc.Segments[0].Custom)
	require.NotNil(t, slc.Segments[1].Custom)
	assert.Equal(t, "./x.sh", slc.Segments[1].Custom.Command)
	assert.Empty(t, slc.Segments[1].Builtin)
}
