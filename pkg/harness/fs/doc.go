// Package fs resolves harness artifacts from directories — the .agents
// layout the users of an agent product already write by hand, or any
// other layout you point it at.
//
// There is one function per artifact kind. Each one takes an [io/fs.FS]
// and the place to look:
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
// Nothing about the .agents layout is built in. It is three lines of
// convention, and a product with its own layout writes its own three.
//
// For skills in a real directory, prefer [SkillsAt] to [Skills]. It
// records the absolute path of each skill, which is what lets the model
// open the support files of the skill.
//
// # Hierarchies
//
// A source — global, user, project — is not a concept here, only a
// directory. A hierarchy is therefore a list of directories, lowest
// first:
//
//	harness.WithSkills(
//	    fs.Skills(os.DirFS("/etc/pi"), ".agents/skills"),
//	    fs.Skills(os.DirFS(homeDir), ".agents/skills"),
//	    fs.Skills(os.DirFS(projectDir), ".agents/skills"),
//	)
//
// A resolver passed to [harness.Harness.Agent] joins that list for one
// build. That is the better route for a per-session project:
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
// as data. The harness synthesizes no tool from them. See [def.Agent].
//
// Skills are directories with a SKILL.md, read whole on every build:
// frontmatter into the metadata, body into [def.Skill.Body]. The body
// stays out of the system prompt. It reaches the conversation through
// the skill tool, not through a later load.
//
// An instructions file is read whole and goes into the system prompt.
//
// # A malformed file is skipped
//
// The tree resolvers — [Agents] and [Skills] — pass over a file they
// can read but cannot understand: no frontmatter block, an unterminated
// one, YAML that will not unmarshal. People write these files by hand,
// and often someone is editing one right now. A half-written definition
// must not fail every session that shares the directory.
//
// The single-file constructors do the opposite. [AgentFile] and
// [SkillDir] name one file, so a caller that asks about that file gets
// the error rather than an empty result.
//
// Only content is forgiven. A file that cannot be read at all —
// permissions, a bad disk — is an error from the walkers too. No amount
// of authoring fixes it, and silence is a lie.
//
// The tolerance has a cost: the skip is silent. Nothing warns that the
// walk passed a file over, and the artifact does not appear. A user
// whose skill went missing reads it with [SkillDir], or a definition
// with [AgentFile]. [harness.Harness.Env] lists what a build did
// resolve.
//
// # Subdirectories are namespaces
//
// [Agents] and [Skills] read a whole tree, and a subdirectory inside it
// is a namespace. A skill at skills/apps/web/deploy is scoped to
// apps/web. The harness qualifies that into the name "apps/web:deploy":
//
//	skills/deploy/SKILL.md            → deploy
//	skills/apps/web/deploy/SKILL.md   → apps/web:deploy
//	skills/apps/api/deploy/SKILL.md   → apps/api:deploy
//
// A monorepo can therefore give each package its own deploy skill, and
// the three do not collide. The model is told which directory each one
// applies to. A skill owns everything below its SKILL.md, so its
// examples/ and scripts/ are support files rather than skills of their
// own.
//
// # Discovery, not application
//
// [Instructions] walks the tree and reports every document it finds,
// each with the directory it governs in [def.Instructions.Dir]. The
// walk visits the whole tree on every build — O(repository),
// node_modules included. If only the root document matters,
// [InstructionsFile] reads that one file and walks nothing.
//
// What [Instructions] does not do is decide when a bound document
// applies. No machinery here watches a run and adds one when the agent
// reaches the directory. [prompt.Default] writes the unbound documents
// into the prompt and leaves the rest in [prompt.Env] for you.
//
// That split is deliberate. A decision like "the agent is working in
// web/ now" means a match of tool arguments against directory names.
// That match has no correct answer. Arguments carry paths in whatever
// form the model wrote them, and a string that looks like a path can
// belong to another repository entirely. The version of this package
// that tried it was more code than everything else here together, and it
// was still wrong at the edges. Discovery is cheap and unambiguous, but
// application is neither, so your product makes that decision.
//
// The cheapest thing to do with the ones you get is nothing. Leave them
// on disk for the agent to read with the file tools it already has, and
// say so in the instructions you do load:
//
//	When you start working in a subdirectory, check for an AGENTS.md
//	there and follow it.
//
// A caller who wants a nested document for certain can write it from a
// builder of their own, at the cost of its tokens in every session.
//
// # Embedded artifacts
//
// Every constructor takes an [io/fs.FS], so defaults compiled into the
// binary resolve exactly like a user directory:
//
//	//go:embed all:defaults
//	var defaults embed.FS
//
//	builtin, _ := iofs.Sub(defaults, "defaults")
//	fs.Skills(builtin, ".agents/skills")
//
// The all: prefix on the embed directive is required. Without it, embed
// skips the dotted .agents directory the convention is built on. The
// embedded root goes first in the list. Built-in agents and skills are
// then available everywhere, and a project can still replace them by
// name.
//
// # Symlinks are not followed
//
// The tree walks use [io/fs.WalkDir], which does not descend into
// symlinked directories. A skills directory that is a symlink farm
// resolves to nothing behind the links. Point resolvers at real paths. A
// symlink to the root itself is fine once resolved, so [os.DirFS] over
// an already-resolved path behaves as expected.
//
// Nothing here caches. Every resolver call re-reads, which is what makes
// a file edited mid-session visible to the next agent build.
package fs
