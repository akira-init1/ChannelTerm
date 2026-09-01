// Package transport defines protocol-specific Channel establishment.
package transport

import (
	"context"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
)

// Transport establishes a protocol-specific Channel.
//
// Implementations validate protocol configuration before Connect. Connect owns
// partially acquired resources until it either returns a Channel or an error.
// The returned Channel owns the established stream independently of the
// Transport that created it.
type Transport interface {
	// Connect establishes and transfers ownership of one live Channel.
	//
	// ctx controls cancellation and the connection deadline. Connect returns a
	// context or protocol error and must release any partially acquired resource.
	Connect(ctx context.Context) (channel.Channel, error)
}
