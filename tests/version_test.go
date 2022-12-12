package tests

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/TrueCloudLab/frostfs-contract/common"
	"github.com/stretchr/testify/require"
)

func TestVersion(t *testing.T) {
	data, err := os.ReadFile("../VERSION")
	require.NoError(t, err)

	v := strings.TrimPrefix(string(data), "v")
	parts := strings.Split(strings.TrimSpace(v), ".")
	require.Len(t, parts, 3)

	var ver [3]int
	for i := range parts {
		ver[i], err = strconv.Atoi(parts[i])
		require.NoError(t, err)
	}

	require.Equal(t, common.Version, ver[0]*1_000_000+ver[1]*1_000+ver[2],
		"version from common package is different from the one in VERSION file")
}
