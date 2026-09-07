package backend

import (
	"context"
	"strings"
)

// QueryAggregatedFreeBusy is the API-level surface — gathers free/busy
// blocks for each attendee email by routing to whichever provider can
// answer for that email's domain. The aggregator favors:
//  1. The user's own identity emails → local DB scan (queryLocalFreeBusy).
//  2. Emails matching the domain of any Google source's user → that
//     source's Google freeBusy.query.
//  3. Emails matching the domain of any Microsoft source's user → Graph
//     getSchedule.
//  4. Fall-through: try every Google + Microsoft source we have; the first
//     that returns non-empty wins. Empty results from every provider are
//     surfaced as a "no data" result rather than misleading "free".
//
// Availability is queried for the exact interval and current sources. Do not
// reuse global email/day results across account changes or edited events.
func (a *API) QueryAggregatedFreeBusy(ctx context.Context, selfEmails, attendeeEmails []string, fromUnix, toUnix int64) ([]FreeBusyResult, error) {
	if len(attendeeEmails) == 0 {
		return nil, nil
	}

	selfSet := make(map[string]struct{}, len(selfEmails))
	for _, e := range selfEmails {
		t := strings.ToLower(strings.TrimSpace(e))
		if t == "" {
			continue
		}
		selfSet[t] = struct{}{}
	}

	sources, err := a.store.ListSources()
	if err != nil {
		return nil, err
	}

	// Pre-resolve provider lists once.
	var googleSources, microsoftSources []Source
	for _, src := range sources {
		if !src.Enabled {
			continue
		}
		switch src.Type {
		case SourceTypeGoogle:
			googleSources = append(googleSources, src)
		case SourceTypeMicrosoft:
			microsoftSources = append(microsoftSources, src)
		}
	}

	out := make([]FreeBusyResult, 0, len(attendeeEmails))
	for _, raw := range attendeeEmails {
		email := strings.ToLower(strings.TrimSpace(raw))
		if email == "" {
			continue
		}

		// Route. Self-emails get the local scan; others fan out across
		// every provider source. The first non-empty answer wins —
		// providers' empty-results-for-foreign-domains are honored.
		var blocks []FreeBusyBlock
		var src string
		if _, isSelf := selfSet[email]; isSelf {
			localBlocks, _ := a.queryLocalFreeBusy(ctx, []string{email}, fromUnix, toUnix)
			if len(localBlocks) > 0 {
				blocks, src = localBlocks, "local"
			}
		}
		if len(blocks) == 0 {
			for _, gs := range googleSources {
				provider := ProviderForSource(gs, ProviderDeps{Store: a.store, Secrets: a.secrets, Auth: a.auth})
				fp, ok := provider.(FreeBusyProvider)
				if !ok {
					continue
				}
				bs, err := fp.QueryFreeBusy(ctx, gs, []string{email}, fromUnix, toUnix)
				if err != nil || len(bs) == 0 {
					continue
				}
				blocks, src = bs, "google"
				break
			}
		}
		if len(blocks) == 0 {
			for _, ms := range microsoftSources {
				provider := ProviderForSource(ms, ProviderDeps{Store: a.store, Secrets: a.secrets, Auth: a.auth})
				fp, ok := provider.(FreeBusyProvider)
				if !ok {
					continue
				}
				bs, err := fp.QueryFreeBusy(ctx, ms, []string{email}, fromUnix, toUnix)
				if err != nil || len(bs) == 0 {
					continue
				}
				blocks, src = bs, "microsoft"
				break
			}
		}

		out = append(out, FreeBusyResult{Email: email, Blocks: blocks, Source: src})
	}

	return out, nil
}
