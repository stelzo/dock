# Dock

Dock is an SSH-based Markdown browser that turns your terminal into a cozy reading nook for when you don't want to open a bloated, corporate, AI-infected mini OS called _webbrowser_ to view or navigate through markdown files. Dock makes your `.md` files look *stunning*, just like Charm's `glow` but with some extra steps.

Try it out, maybe you'll like it!

```bash
ssh marina@steado.tech
```

**Why would you do this?**
- **Keyboard first:** Stay in the terminal. No distractions. Just _beautiful_ docs. Your mouse is still supported 🐁
- **SSH is cool:** No bloated browser but you still get all information and probably all functionality the web docs would give you without installing anything.
- **Eye Candy ✨:** Powered by the glamourous charm libraries and kitty/sixel terminal image protocols! If you have a smooth terminal setup, dock will just fit in. It comes with gorgeous themes (Nord, Dracula, Tokyo Night, and more!) to keep your eyes happy. Just use it as a subdomain. Seriously. `ssh pink.marina@steado.tech` just works.

---

## 🛠️ Get it on your machine (or server)

If you are just using dock remotely, there is no need to install anything except ssh. Just ssh to the server provided by the project:
```bash
ssh docs.project.com
```

For running dock locally or as a SSH server for your own docs, install via Go:

```bash
go install codeberg.org/stelzo/dock@latest
```

---

## 🎮 How to use it

### 🌐 Server Mode
Host your docs for the world (or just your team) to see:
```bash
dock serve ./my-awesome-docs --port 2222
```
Dock will automatically detect `dock.toml`, zensical or mkdocs configurations and populate the navigation ordering and project name.

### 🕵️ Global Search & Retrieval
Quick and dirty for your scripts or your clanker:
```bash
ssh -p 2222 localhost search ./docs "how to fly"
ssh -p 2222 dock get ./docs "getting started"
```

### 🏠 Local Mode
Just point it at a folder full of Markdown files:
```bash
dock ./my-awesome-docs
```

### 🔗 Remote Client Mode
Connect to a remote dock instance directly:
```bash
dock marina@localhost -p 2222
```
If you provide a `--theme` flag or have `DOCK_THEME` set, it will automatically be prefixed to your username (e.g. `pink.marina@localhost`) to select the theme on the remote server.

---

## 🎨 Dress to Impress (Themes)

We've got vibes for every mood. You can set `DOCK_THEME` or just pick one when you connect via SSH.

**The Current Wardrobe:**
- 🌑 **Dark** (The classic from Charm)
- 🗼 **Tokyo Night** (Neon vibes)
- 🧛 **Dracula** (For the night owls)
- ❄️ **Nord** (Cool and crisp)
- ☕ **Catppuccin Mocha** (Smooth & cozy)
- 🌸 **Pink** (Kawaii docs)
- 🏳️‍⚧️ **Trans** (Rights!)
- ☀️ **Light** (For those who like the sun)

**Pro Tip:** To connect with a specific theme over SSH:
```bash
ssh -p 2222 nord@localhost
```

---

## ⚙️ Config

### 📄 Config File

Dock reads navigation order and project metadata from a config file in your docs directory (or its parent). It supports the following formats, checked in priority order:

1. `dock.toml` / `.dock.toml`
2. `zensical.toml`
3. `mkdocs.yml` / `mkdocs.yaml`

**Example `dock.toml`:**
```toml
site_name = "My Awesome Docs"
dock_dir = "docs"
nav = [
    { Introduction = "index.md" }, # Root-level page
    { "Getting Started" = [        # Section header
        { Installation = "setup/install.md" }, # Sub-page
        { "Quick Start" = "setup/quickstart.md" }
    ]},
    { Reference = "reference.md" }
]
```

**Example `zensical.toml`:**
```toml
[project]
site_name = "My Awesome Docs"
docs_dir = "docs"

# Each block in nav is either a page or a section header
[[project.nav]]
Introduction = "index.md"

[[project.nav]]
"Getting Started" = [
  { Installation = "setup/install.md" },
  { "Quick Start" = "setup/quickstart.md" },
]
```

- `site_name`: Project name shown at the top of the nav panel.
- `dock_dir`: Path to docs folder, relative to the config file (defaults to the config file's directory).
- `nav`: Ordered list of pages and sections. A string value defines a clickable page, while an array value defines a section header with nested sub-pages.
- `git_url`: Git URL to clone/pull documentation from.
- `git_ref`: Git branch or tag to use (defaults to default branch).
- `pull_interval`: Interval between pulls (e.g., `1h`, `30m`).
- `cache_path`: Local directory to store the cloned repository.

### 🌍 Environment Variables

- `DOCK_TITLE`: Override the project name shown in the nav.
- `DOCK_THEME`: Your default vibe.
- `DOCK_IGNORE_DIRS`: Folders you want to hide (Default: `assets,stylesheets`).
- `DOCK_SSH_PORT`: Default port for both serving and client connections (Default: `22`).
- `DOCK_GIT_URL`: Git repository to sync from.
- `DOCK_GIT_REF`: Branch or tag to use.
- `DOCK_PULL_INTERVAL`: How often to pull updates.
- `DOCK_CACHE_PATH`: Custom path for Git cache.
