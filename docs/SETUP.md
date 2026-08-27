# Local environment setup

This machine has no Homebrew, Go, Node, Docker, or `gh` installed. The steps
below need your interactive input (sudo password / browser login), so run them
yourself in the terminal — prefix each with `!` if running from inside Claude
Code so the output comes back into the session.

## 1. Homebrew
```
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```
Follow the prompts at the end to add brew to your PATH (it will print the
exact `echo ... >> ~/.zprofile` lines for Apple Silicon).

## 2. Toolchain via Homebrew
```
brew install go node gh docker docker-compose uv
```

## 3. GitHub CLI auth
```
gh auth login
```
Choose GitHub.com → HTTPS → login with a web browser. This is what lets me
create the private repo on your account later.

## 4. Docker Desktop
`brew install docker docker-compose` installs the CLI only. If you don't
already have Docker Desktop, install it from https://www.docker.com/products/docker-desktop/
and start it once so the daemon is running (needed for the Postgres container).

## 5. Spec Kit (SDD CLI)
```
uv tool install specify-cli --from git+https://github.com/github/spec-kit.git
specify check
```

## After this is done
Come back and tell me — I'll run `specify init`, create the private GitHub
repo, scaffold `docker-compose.yml` for Postgres, and start the Go module.
