// Package fs resolves harness artifacts from directories — the .agents
// layout an agent product's users already write by hand, or any other
// layout you point it at.
//
// There is one function per artifact kind, each taking an [io/fs.FS] and
// the place to look:
//
//	proj := os.DirFS(projectDir)
//
//	harness.New(
//	    harness.WithCatalog(cat),
//	    harness.WithDefaultModel("anthropic/claude-sonnet-4-6"),
//	    harness.WithAgents(fs.Agents(proj, ".agents/agents")),
//	    harness.WithSkills(fs.Skills(proj, ".agents/skills")),
//	    harness.WithInstructions(fs.Instructions(proj, "AGENTS.md")),
//	)
//
// Nothing about the .agents layout is built in: it is three lines of
// convention, and a product with its own layout writes its own three.
//
// For skills in a real directory, prefer [SkillsAt] to [Skills]. It
// records each skill's absolute path, which is what lets the model open
// the skill's support files.
//
// # Hierarchies
//
// A source — global, user, project — is not a concept here, only a
// directory. So a hierarchy is a list of them, lowest first:
//
//	harness.WithSkills(
//	    fs.Skills(os.DirFS("/etc/pi"), ".agents/skills"),
//	    fs.Skills(os.DirFS(homeDir), ".agents/skills"),
//	    fs.Skills(os.DirFS(projectDir), ".agents/skills"),
//	)
//
// Passing a resolver to [harness.Harness.Agent] appends it to that list
// for one build, which is the better route for a per-session project:
//
//	a, err := h.Agent(ctx,
//	    durable.WithSessionID(id),
//	    harness.WithWorkDir(repoPath),
//	    harness.WithInstructions(fs.Instructions(os.DirFS(repoPath), "AGENTS.md")),
//	)
//
// # What is read
//
// Agent definitions are markdown files with YAML frontmatter, resolved
// as data — the harness synthesizes no tool from them; see [def.Agent].
// Skills are directories with a SKILL.md, read whole on every build:
// frontmatter into the metadata, body into [def.Skill.Body]. What stays
// out of the system prompt is the body — it reaches the conversation
// through the skill tool, not by being loaded later. An instructions
// file is read whole and goes into the system prompt.
//
// # A malformed file is skipped
//
// The tree resolvers — [Agents] and [Skills] — pass over a file they
// can read but not understand: no frontmatter block, an unterminated
// one, YAML that will not unmarshal. These files are authored by hand,
// often by someone editing one right now, and a half-written definition
// must not fail every session that shares the directory.
//
// The single-file constructors do the opposite. [AgentFile] and
// [SkillDir] name one file, so a caller asking about that file gets the
// error rather than an empty result.
//
// Only content is forgiven. A file that cannot be read at all —
// permissions, a failing disk — is an error from the walkers too,
// because no amount of authoring fixes it and silence would be a lie.
//
// The cost of the tolerance is that skipping is silent: nothing warns
// that a file was passed over, and the artifact simply does not appear.
// A user whose skill went missing checks it with [SkillDir] (or a
// definition with [AgentFile]), and [harness.Harness.Env] lists what a
// build did resolve.
//
// # Subdirectories are namespaces
//
// [Agents] and [Skills] read a whole tree, and a subdirectory inside it
// is a namespace. A skill at skills/apps/web/deploy is scoped to
// apps/web, which the harness qualifies into the name "apps/web:deploy":
//
//	skills/deploy/SKILL.md            → deploy
//	skills/apps/web/deploy/SKILL.md   → apps/web:deploy
//	skills/apps/api/deploy/SKILL.md   → apps/api:deploy
//
// So a monorepo can give each package its own deploy skill without the
// three colliding, and the model is told which directory each applies
// to. A skill owns everything below its SKILL.md, so its examples/ and
// scripts/ are support files rather than skills of their own.
//
// # Discovery, not application
//
// [Instructions] walks the tree and reports every document it finds,
// each with the directory it governs in [def.Instructions.Dir]. The
// walk visits the whole tree on every build — O(repository),
// node_modules included — so when only the root document matters,
// [InstructionsFile] reads that one file and walks nothing.
//
// What [Instructions] does not do is decide when a bound document
// applies. There is no
// machinery here that watches a run and injects one when the agent
// reaches the directory — [prompt.Default] renders the unbound documents
// and leaves the rest in [prompt.Env] for you.
//
// That split is deliberate. Deciding "the agent is working in web/ now"
// means matching tool arguments against directory names, and that match
// has no correct answer: arguments carry paths in whatever form the
// model wrote them, and a string that looks like a path may belong to
// another repository entirely. The version of this package that tried it
// was more code than everything else here combined, and it was still
// wrong at the edges. Discovery is cheap and unambiguous; application is
// neither, so application is a decision your product makes.
//
// The cheapest thing to do with the ones you get is nothing — leave them
// on disk for the agent to read with the file tools it already has, and
// say so in the instructions you do load:
//
//	When you start working in a subdirectory, check for an AGENTS.md
//	there and follow it.
//
// A caller who wants a nested document guaranteed can render it from a
// builder of their own, at the cost of its tokens in every session.
//
// # Embedded artifacts
//
// Every constructor takes an [io/fs.FS], so defaults compiled into the
// binary resolve exactly like a user's directory:
//
//	//go:embed all:defaults
//	var defaults embed.FS
//
//	builtin, _ := iofs.Sub(defaults, "defaults")
//	fs.Skills(builtin, ".agents/skills")
//
// The all: prefix on the embed directive is required — without it embed
// skips the dotted .agents directory the convention is built on. Listing
// the embedded root first keeps built-in agents and skills available
// everywhere while letting a project replace them by name.
//
// # Symlinks are not followed
//
// The tree walks use [io/fs.WalkDir], which does not descend into
// symlinked directories — a skills directory that is a symlink farm
// will resolve to nothing behind the links. Point resolvers at real
// paths; a symlink to the root itself is fine once resolved, so
// [os.DirFS] over an already-resolved path behaves as expected.
//
// Nothing here caches: every resolver call re-reads, which is what makes
// a file edited mid-session visible to the next agent build.
package fs
