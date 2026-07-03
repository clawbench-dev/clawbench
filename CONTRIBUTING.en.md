[中文](CONTRIBUTING.md) | [English](CONTRIBUTING.en.md)

# Contributing Guide

Thank you for your interest in ClawBench! Here's how you can contribute:

- 🐛 [Report a Bug](../../issues/new?template=bug_report.yml)
- 💡 [Request a Feature](../../issues/new?template=feature_request.yml)
- ❓ [Ask a Question](../../issues/new?template=question.yml)
- 🔧 Submit code or documentation

## Code of Conduct

- Be respectful and constructive
- Chinese is the primary language; English is also welcome
- Add a space between Chinese and English text for readability

---

## How to Contribute

### Report a Bug

1. Use the [Bug Report template](../../issues/new?template=bug_report.yml)
2. Provide clear reproduction steps, expected behavior, and actual behavior
3. Include environment info (version, OS, browser/Android version)
4. Attach logs or screenshots if possible

### Request a Feature

1. Use the [Feature Request template](../../issues/new?template=feature_request.yml)
2. Describe the problem you're facing, not just the solution you want
3. Mention any alternative approaches you've considered

### Ask a Question

Use the [Question template](../../issues/new?template=question.yml) or [GitHub Discussions](../../discussions).

---

## Development Workflow

### Prerequisites

- Go 1.25+
- Node.js 22+
- JDK 17 (for Android development)

```bash
git clone https://github.com/xulongzhe/clawbench.git
cd clawbench
```

See [AGENTS.md](AGENTS.md) for build and run commands.

### Branch Strategy

| Branch | Purpose |
|--------|---------|
| `main` | Stable branch — only merged via PR |
| `feat/<desc>` | New features |
| `fix/<desc>` | Bug fixes |
| `docs/<desc>` | Documentation updates |

### Develop → Commit → PR

1. Create a branch from `main`: `git checkout -b feat/your-feature`
2. Develop and commit (follow the Commit Convention below)
3. Push and create a PR
4. Wait for CI to pass and the PR to be merged

---

## Commit Convention

### Format

```
<type>(<scope>): <description>
```

- **type**: English, required
- **scope**: English module name, optional
- **description**: Chinese preferred; add spaces between CJK and Latin text; no period for Chinese

### Types

| Type | Purpose |
|------|---------|
| `feat` | New feature |
| `fix` | Bug fix |
| `docs` | Documentation |
| `style` | Code formatting (no logic change) |
| `refactor` | Refactoring (not a feature or fix) |
| `perf` | Performance improvement |
| `test` | Test cases |
| `chore` | Build/tools/dependencies |
| `ci` | CI configuration |
| `build` | Build output/scripts |
| `revert` | Revert a commit |

### Scopes (reference)

`android`, `rag`, `task`, `scheduler`, `tts`/`speech`, `ssh`, `ws`, `terminal`, `config`, `ci`

### Examples

```
feat(android): push notifications show AI reply preview
docs: add RAG deployment documentation
refactor(scheduler): optimize scheduled task dispatch logic
test: improve backend test coverage — internal/handler
chore: upgrade Go version to 1.25
```

---

## PR Convention

### Title Format

Same as commit messages:

```
<type>(<scope>): <description>
```

### Description Template

PRs are created with a template that includes:

- **Changes**: What and why
- **Change type**: Check the relevant type
- **Linked Issue**: `Fixes #N` / `Closes #N`
- **Testing**: Test results and verification method
- **Checklist**: Code style, no secrets, docs updated

### Rules

- Must link to an Issue
- CI must pass (Go / Frontend / Android coverage gates)
- Squash Merge into main

---

## Code Style

- **Go**: `gofmt` + `go vet`
- **Vue / TypeScript**: Follow existing project configuration
- Changes must have test coverage

## Testing

```bash
go test ./...        # All Go tests
npm test             # All frontend tests
```

CI enforces a coverage gate:
- **Tier 1**: Per-package/directory coverage must not fall below baseline - 1.5%
- **Tier 2**: Changed-line coverage must be ≥ 80%

See the "Coverage gate" section in [AGENTS.md](AGENTS.md) for details.

## Release Process

- Git tag-driven; `release.yml` auto-builds multi-platform artifacts
- Squash merge to main → tag → auto-publish

## License

This project is licensed under the [MIT License](LICENSE). By submitting a contribution, you agree that your code will be released under the MIT License.
