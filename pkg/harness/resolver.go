package harness

import (
	"context"
	"fmt"
	"strings"

	"github.com/sonnes/pi-go/pkg/harness/def"
)

// resolution is the artifact snapshot for one agent build: what every
// registered resolver returned, qualified and merged.
type resolution struct {
	agents       []def.Agent
	skills       []def.Skill
	instructions []def.Instructions
}

// resolve calls the configured resolvers and checks the names they
// return. It runs on every build — each [Harness.Agent] and
// [Harness.Env] call — and never caches: a resolver that wants caching
// implements it internally, where it knows what invalidates.
//
// Sources are consulted in registration order, lowest first, and their
// results union. A scoped artifact is qualified before the union, so a
// name from one directory does not displace the same name from another;
// what remains colliding is the same artifact declared twice, and the
// highest source wins.
func (b *build) resolve(ctx context.Context) (*resolution, error) {
	agents, err := b.resolveAgents(ctx)
	if err != nil {
		return nil, err
	}
	skills, err := b.resolveSkills(ctx)
	if err != nil {
		return nil, err
	}
	docs, err := b.resolveInstructions(ctx)
	if err != nil {
		return nil, err
	}

	return &resolution{agents: agents, skills: skills, instructions: docs}, nil
}

// resolveAgents unions the agent definitions, checking names only. A
// definition's Model and Tools are advisory metadata the harness never
// consumes, so validating them here would fail builds on behalf of no
// consumer — the product that acts on a definition checks what it uses.
func (b *build) resolveAgents(ctx context.Context) ([]def.Agent, error) {
	var out []def.Agent
	at := make(map[string]int)

	for i, r := range b.agents {
		list, err := r.Agents(ctx)
		if err != nil {
			return nil, fmt.Errorf("harness: agent resolver %d (%T): %w", i, r, err)
		}

		seen := make(map[string]bool, len(list))
		for _, a := range list {
			name, err := claim(a.Name, a.Scope, a.Source, "agent", i, r, seen)
			if err != nil {
				return nil, err
			}
			a.Name = name
			out = place(out, at, name, a)
		}
	}
	return out, nil
}

func (b *build) resolveSkills(ctx context.Context) ([]def.Skill, error) {
	var out []def.Skill
	at := make(map[string]int)

	for i, r := range b.skills {
		list, err := r.Skills(ctx)
		if err != nil {
			return nil, fmt.Errorf("harness: skill resolver %d (%T): %w", i, r, err)
		}

		seen := make(map[string]bool, len(list))
		for _, s := range list {
			name, err := claim(s.Name, s.Scope, s.Source, "skill", i, r, seen)
			if err != nil {
				return nil, err
			}
			s.Name = name
			out = place(out, at, name, s)
		}
	}
	return out, nil
}

// resolveInstructions concatenates every document every source returns.
// Documents have no name, so there is nothing to qualify and nothing to
// override — a house document and a project document both apply.
//
// A document whose content exactly matches one already collected is
// skipped: registering the same file twice — one root nested inside
// another, a resolver listed at two sites — would otherwise pay for it
// in every prompt, silently.
func (b *build) resolveInstructions(ctx context.Context) ([]def.Instructions, error) {
	var out []def.Instructions
	seen := make(map[string]bool)

	for i, r := range b.instructions {
		list, err := r.Instructions(ctx)
		if err != nil {
			return nil, fmt.Errorf("harness: instruction resolver %d (%T): %w", i, r, err)
		}
		for _, doc := range list {
			if seen[doc.Content] {
				continue
			}
			seen[doc.Content] = true
			out = append(out, doc)
		}
	}
	return out, nil
}

// claim returns the name an artifact is known by: its own, qualified by
// the directory it governs. It also rejects the ways a single source
// can be malformed — an unnamed artifact, a name that spells its own
// ":" qualifier, and a name handed over twice. A duplicate here is not
// an override; overriding is between sources, never within one.
func claim(
	name, scope, source, kind string,
	at int,
	resolver any,
	seen map[string]bool,
) (string, error) {
	if name == "" {
		return "", fmt.Errorf(
			"harness: %s resolver %d (%T): %s has no name (source %q)",
			kind, at, resolver, kind, source,
		)
	}
	// ":" is how qualification is spelled, so a literal one would let
	// this artifact collide with a scoped name from somewhere else.
	if strings.Contains(name, ":") {
		return "", fmt.Errorf(
			"harness: %s resolver %d (%T): %s name %q contains %q, which is reserved for scope qualification (source %q)",
			kind, at, resolver, kind, name, ":", source,
		)
	}

	qualified := name
	if scope != "" {
		qualified = scope + ":" + name
	}
	if seen[qualified] {
		// The name a model sees is not always the name on disk, so the
		// error prints both, plus the scope that joins them.
		return "", fmt.Errorf(
			"harness: %s resolver %d (%T): duplicate %s %q (scope %q, qualified %q, source %q)",
			kind, at, resolver, kind, name, scope, qualified, source,
		)
	}
	seen[qualified] = true
	return qualified, nil
}

// place adds an artifact under a name a lower source already claimed, or
// at the end when the name is new. Replacing in place keeps a name where
// it first appeared, so adding an override does not reshuffle the list
// the model is shown.
func place[T any](out []T, at map[string]int, name string, item T) []T {
	if pos, ok := at[name]; ok {
		out[pos] = item
		return out
	}
	at[name] = len(out)
	return append(out, item)
}
