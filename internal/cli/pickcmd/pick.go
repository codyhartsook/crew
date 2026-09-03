// Package pickcmd chooses which agent should get the next piece of work.
package pickcmd

import (
	"context"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/usage"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		cheapest bool
		verbose  bool
	)

	cmd := &cobra.Command{
		Use:   "pick",
		Short: "Name the agent here with the most room to work",
		Long: `Prints one agent name, chosen by how much context window it has left,
or with --cheapest by how little its model costs per token.

Compose it with post to hand work to whoever can best take it:

  crew post handoff --to "$(crew pick)" "..."

Only agents that have reported usage can be compared, so an agent whose harness
reports nothing is never picked over one that does.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()

			self, _ := rc.Author(cmd.Context(), "")
			ranked, err := candidates(cmd.Context(), rc, self)
			if err != nil {
				return err
			}
			if len(ranked) == 0 {
				return fmt.Errorf("no other agent here has reported usage yet")
			}
			sortBy(ranked, cheapest)

			out := cmd.OutOrStdout()
			if !verbose {
				fmt.Fprintln(out, ranked[0].name)
				return nil
			}
			for _, c := range ranked {
				fmt.Fprintf(out, "%-14s %3.0f%% free  %s\n", c.name, c.headroom*100, c.rate)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&cheapest, "cheapest", false, "rank by model price instead of free context")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "show every candidate and why")
	return cmd
}

// candidate is one agent we could send work to.
type candidate struct {
	name     string
	headroom float64
	// multiplier is the model's price relative to the cheapest we know. Zero
	// when unpriced, which sorts last rather than looking free.
	multiplier float64
	rate       string
}

// candidates lists the agents in this room whose usage we can compare, self
// excluded: handing work to yourself is not a handoff.
func candidates(ctx context.Context, rc *roomctx.Context, self string) ([]candidate, error) {
	active, err := rc.Store.List(ctx, store.Filter{Status: session.StatusActive})
	if err != nil {
		return nil, err
	}
	var out []candidate
	for _, s := range active {
		if s.Key() == self || s.Usage == nil || !rc.Contains(s) {
			continue
		}
		headroom, ok := s.Usage.Headroom()
		if !ok {
			continue
		}
		c := candidate{name: room.Display(s), headroom: headroom, rate: "price unknown"}
		if m, ok := usage.Multiplier(s.Usage.Model); ok {
			c.multiplier = m
			c.rate = fmt.Sprintf("%.3gx cheapest", m)
		}
		out = append(out, c)
	}
	return out, nil
}

// sortBy orders candidates best first. Each key breaks ties with the other, so
// two agents on the same model are separated by their free context.
func sortBy(cs []candidate, cheapest bool) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if cheapest {
			if a.multiplier != b.multiplier {
				return cheaper(a.multiplier, b.multiplier)
			}
			return a.headroom > b.headroom
		}
		if a.headroom != b.headroom {
			return a.headroom > b.headroom
		}
		return cheaper(a.multiplier, b.multiplier)
	})
}

// cheaper compares model prices, treating an unknown price as the most
// expensive: not knowing what a model costs is no evidence that it is cheap.
func cheaper(a, b float64) bool {
	switch {
	case a == 0:
		return false
	case b == 0:
		return true
	default:
		return a < b
	}
}
