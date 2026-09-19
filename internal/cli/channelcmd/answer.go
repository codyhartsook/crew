package channelcmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
)

func NewAnswer(opts *cmdutil.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "answer <id> <body>",
		Short: "Answer a waiting delegated role",
		Long:  "Releases the role blocked on `crew ask`. A request takes one answer.",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid request id %q", args[0])
			}
			st, cs, err := channelStore(opts)
			if err != nil {
				return err
			}
			defer st.Close()

			// Unverified in v1: one derived addressee, and a person may answer
			// for a busy session.
			answered, err := cs.Answer(cmd.Context(), id, strings.Join(args[1:], " "))
			if err != nil {
				return err
			}
			if !answered {
				r, getErr := cs.GetRequest(cmd.Context(), id)
				if getErr != nil {
					return getErr
				}
				return fmt.Errorf("request [%d] was already answered: %s", r.ID, r.Answer)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "answered [%d]\n", id)
			return nil
		},
	}
	return cmd
}
