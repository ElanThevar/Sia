package client

import (
	"strconv"

	"github.com/NebulousLabs/Sia/node/api"
	"github.com/NebulousLabs/Sia/types"
)

// ConsensusGet requests the /consensus api resource
func (c *Client) ConsensusGet() (cg api.ConsensusGET, err error) {
	err = c.Get("/consensus", &cg)
	return
}

// ConsensusBlocksIDGet requests the /consensus/blocks api resource
func (c *Client) ConsensusBlocksIDGet(id types.BlockID) (block types.Block, err error) {
	err = c.Get("/consensus/blocks?id="+id.String(), &block)
	return
}

// ConsensusBlocksHeightGet requests the /consensus/blocks api resource
func (c *Client) ConsensusBlocksHeightGet(height types.BlockHeight) (block types.Block, err error) {
	err = c.Get("/consensus/blocks?height="+strconv.FormatUint(uint64(height), 10), &block)
	return
}
