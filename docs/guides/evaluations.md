# Evaluations


Try Atomic Claude in an isolated Docker container. Builds `atomic` from this repo's source, lays the bundle into a persistent `~/.claude`, drops you into Claude Code in a workspace dir that survives container removal.

Prereq: Docker + docker compose v2.


## Contributors (working in this repo)

Build the image once:

    make docker-build

Then drop into the Claude TUI:

    make docker-up

To bypass the entrypoint for fast iteration (raw bash shell, no Claude TUI):

    make docker-shell


## End users


If you're not a contributor to this repo and want to evaluate atomic-claude on your own project, install atomic, then:

    atomic docker init

Writes `Dockerfile`, `docker-compose.yml`, `docker-entrypoint.sh`, `.dockerignore`, and a `tmp/` scaffold into `./atomic-docker/` (override with `--target some/path`). Refuses to overwrite existing files unless `--force` is passed.

From there:

    cd atomic-docker
    docker compose build
    docker compose run --rm atomic-eval

Drop your project files into `atomic-docker/tmp/workspace/` (or symlink your repo into it). Same volume layout and first-run `claude login` flow as the contributor setup above.


## Volume layout

Two directories under `tmp/` are bind-mounted into the container:

- `tmp/workspace/` → `/workspace` inside the container. Your eval project lives here. Persists across `docker compose run` invocations; only `.gitkeep` is tracked in git.
- `tmp/claude-home/` → `/home/atomic/.claude` inside the container. Holds Claude config, memory, and auth tokens. Persists `claude login` across runs. Only `.gitkeep` is tracked in git.

Both are gitignored. The `.gitkeep` placeholders keep them in the repo so the bind mounts exist on a fresh clone.


## First-run auth

On first `make docker-up`, Claude Code prompts you to authenticate. It emits a URL and code; open the URL in your host browser and paste the code. Auth tokens land in `tmp/claude-home/` and persist. Subsequent `make docker-up` runs skip the prompt.


## Linux UID note

Bind mounts use the host UID. On Linux, if `tmp/` files end up root-owned, rebuild with your UID:

    make docker-build HOST_UID=$(id -u)

Mac and Windows Docker Desktop handle UID mapping transparently; this step is not needed there.


## Reset

To start fresh (wipes auth and workspace):

    rm -rf tmp/claude-home/* tmp/claude-home/.[!.]* tmp/workspace/* tmp/workspace/.[!.]* 2>/dev/null; touch tmp/claude-home/.gitkeep tmp/workspace/.gitkeep
