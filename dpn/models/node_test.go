package models_test

import (
	"github.com/APTrust/exchange/dpn/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestChooseNodesForReplication(t *testing.T) {
	node := testutil.MakeDPNNode()
	for i := 1; i <= 4; i++ {
		selectedNodes, err := node.ChooseNodesForReplication(i, "")
		assert.Nil(t, err)
		assert.Equal(t, i, len(selectedNodes))
	}
	selectedNodes, err := node.ChooseNodesForReplication(1000, "")
	assert.NotNil(t, err)
	require.NotNil(t, selectedNodes)
	assert.Empty(t, selectedNodes)

	for i := 1; i <= 10; i++ {
		selectedNodes, err := node.ChooseNodesForReplication(2, "hathi")
		assert.Nil(t, err)
		require.NotNil(t, selectedNodes)
		assert.Equal(t, 2, len(selectedNodes))
		for j := 0; j < len(selectedNodes); j++ {
			assert.True(t, selectedNodes[j] != "hathi")
		}
	}
}

func TestNodeFQDN(t *testing.T) {
	node := testutil.MakeDPNNode()

	node.APIRoot = "https://example.com"
	host, err := node.FQDN()
	assert.Nil(t, err)
	assert.Equal(t, "example.com", host)

	node.APIRoot = "https://abc.example.com:8080"
	host, err = node.FQDN()
	assert.Nil(t, err)
	assert.Equal(t, "abc.example.com", host)

}
