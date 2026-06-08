# Evaluations

Try Atomic Claude in an isolated Docker container before installing it on your machine. The container builds `atomic` from source, lays the bundle into a persistent `~/.claude`, and drops you into Claude Code with a workspace that survives container removal.

**Prerequisite:** Docker + docker compose v2.


## For contributors

If you are working in this repo:

```bash
make docker-build    # build the image (once)
make docker-up       # start Claude Code in the container
```

For a raw shell without the Claude TUI (useful for fast iteration):

```bash
make docker-shell
```


## For everyone else

If you want to evaluate atomic-claude on your own project without cloning this repo:

```bash
atomic docker init
cd atomic-docker
docker compose build
docker compose run --rm atomic-eval
```

`atomic docker init` writes a self-contained Docker setup into `./atomic-docker/` (override with `--target`). Drop your project files into `atomic-docker/tmp/workspace/` or symlink your repo into it.

To evaluate the code-intelligence engine, drop a project into the workspace, then inside the container run `atomic code index` followed by `atomic code explore "<question about the codebase>"`. The response shows the symbols, files, and call relationships resolved from the real graph rather than from grep, which is the clearest way to see what the engine adds.


## How volumes work

Two directories under `tmp/` are mounted into the container:

| Host path | Container path | What lives here |
|-----------|---------------|----------------|
| `tmp/workspace/` | `/workspace` | Your project. Persists across runs. |
| `tmp/claude-home/` | `/home/atomic/.claude` | Claude config, auth tokens, memory. Persists `claude login` across runs. |

Both are gitignored. The `.gitkeep` placeholders keep them in the repo so the mounts work on a fresh clone.


## First-run authentication

On first launch, Claude Code prompts you to authenticate. It shows a URL and a code — open the URL in your host browser and paste the code. Auth tokens are saved in `tmp/claude-home/` and persist. Subsequent launches skip the prompt.


## Linux UID note

Bind mounts use the host UID. If files end up root-owned on Linux, rebuild with your UID:

```bash
make docker-build HOST_UID=$(id -u)
```

Mac and Windows Docker Desktop handle this transparently.


## Reset

To wipe everything and start fresh:

```bash
rm -rf tmp/claude-home/* tmp/claude-home/.[!.]* tmp/workspace/* tmp/workspace/.[!.]*
touch tmp/claude-home/.gitkeep tmp/workspace/.gitkeep
```
