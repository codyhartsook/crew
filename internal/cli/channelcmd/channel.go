// Package channelcmd is the directed back-channel: a delegated role asking the
// agent that delegated to it for context, and that agent answering.
package channelcmd

import (
	"errors"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// channelStore opens the local database; the HTTP store carries no channel.
func channelStore(opts *cmdutil.Options) (store.Store, store.ChannelStore, error) {
	if opts.Server != "" {
		return nil, nil, errors.New("the back-channel needs the local database; unset --server")
	}
	st, err := opts.OpenStore()
	if err != nil {
		return nil, nil, err
	}
	cs, ok := st.(store.ChannelStore)
	if !ok {
		st.Close()
		return nil, nil, errors.New("this store does not support the back-channel")
	}
	return st, cs, nil
}
