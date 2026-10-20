# Terminal shell integration and quick-command suggestions (JUS-249)

Saved quick commands work in every shell: the editor stores a label, the
command text and an optional abbreviation in this browser for the signed-in
account, and **Insert** puts the text on the prompt without pressing Enter.

Abbreviation suggestions are different. xterm emulates a terminal; it is not
the shell's editable command-line buffer. Gotham therefore only offers a
suggestion when the shell itself says "an editable prompt is ready", using the
prompt marks defined by [OSC 133](https://gitlab.freedesktop.org/gnome/vte/-/blob/master/doc/OSC-133.md)
(FinalTerm/iTerm2) or OSC 633 (VS Code). Gotham never parses prompt text,
never concatenates keystrokes to guess remote state, and never writes a setup
command to the PTY.

## What the marks do

| Mark | Meaning | Gotham behaviour |
|---|---|---|
| `OSC 133 ; A` | prompt starts | pause suggestions |
| `OSC 133 ; B` | prompt ends; the command line is editable | enable conservative suggestion matching |
| `OSC 133 ; C` | the user submitted the command | disable suggestions |
| `OSC 133 ; D ; <status>` | command finished | stay disabled until the next `B` |

OSC 633 uses the same letters and is accepted with the same meaning.

The local mirror is intentionally narrow. It advances only on one printable
ASCII character or Backspace, and is discarded on every other input chunk:
cursor keys/Home/End (escape sequences), Ctrl-C, Enter, paste, IME
composition, or any control byte. Entering the alternate screen (`?1049h`,
`?1047h`, `?47h`) also disables suggestions, so fullscreen TUIs and password
prompts (which never print a shell prompt `B` mark) cannot be captured.

Accepting a suggestion sends **Backspace × the typed abbreviation length** and
then the command text. It never sends Enter. The count is safe because the
mirror proves the abbreviation is the last text before the cursor.

## Supported shells

| Shell | Status | Notes |
|---|---|---|
| bash ≥ 4.4 | **Supported, PTY-tested** | Requires `PS0` (bash 4.4) and an interactive `--rcfile`/`~/.bashrc` snippet below. |
| zsh ≥ 5.3 | **Supported with the snippet below** | Uses `add-zsh-hook precmd/preexec`; themes that rebuild `PROMPT` after the hook may need the `B` mark appended in their own `precmd`. |
| fish | **Not supported in JUS-249** | No tested integration is shipped; suggestions stay off, manual insertion still works. |
| sh / dash | **Not supported** | No prompt hook or `PS0`; suggestions stay off, manual insertion still works. |
| ksh / others | **Not supported** | Same as sh/dash. |

"Not supported" is a clean state, not an error: the terminal keeps working,
typing is forwarded unchanged, and the quick-command panel shows the setup
hint without offering a snippet for an untested shell.

## bash setup (additive, optional)

Append to `~/.bashrc` (or the file passed with `--rcfile`). The snippet keeps
the existing prompt and `PROMPT_COMMAND`; it never runs a command for the
user. Bash 5.1+ allows `PROMPT_COMMAND` to be an array; if you use an array,
add `__gotham_prompt_command` as its first element instead of the string
assignment below.

```bash
# ~/.bashrc — optional Gotham prompt marks (bash >= 4.4).
__gotham_prompt_command() {
  local status=$?
  printf '\033]133;D;%s\a' "$status"
  printf '\033]133;A\a'
  return $status
}
PROMPT_COMMAND="__gotham_prompt_command${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
PS1="${PS1}\[\033]133;B\a\]"
PS0="\[\033]133;C\a\]${PS0:-}"
```

## zsh setup (additive, optional)

Append to `~/.zshrc` after your theme/hook setup. `add-zsh-hook` appends the
function to the existing hook list, and the `B` mark is added to `PROMPT` once
(guarded by a test) so repeated prompts do not accumulate escape bytes.

```zsh
# ~/.zshrc — optional Gotham prompt marks (zsh >= 5.3).
__gotham_precmd() {
  local status=$?
  print -n $'\e]133;D;'"$status"$'\a\e]133;A\a'
  if [[ $PROMPT != *$'\e]133;B\a'* ]]; then
    PROMPT="${PROMPT}"$'%{\e]133;B\a%}'
  fi
}
__gotham_preexec() {
  print -n $'\e]133;C\a'
}
autoload -Uz add-zsh-hook
add-zsh-hook precmd __gotham_precmd
add-zsh-hook preexec __gotham_preexec
```

## What this build does not do

* It does not start the shell with an integration rcfile. Starting bash with
  `--rcfile` (and zsh with an additive `ZDOTDIR` rc) belongs to the node agent,
  which currently runs `exec.Command(shell)` with no arguments. That follow-up
  must be additive and optional: source the operator's normal rc first, add the
  marks last, and never inject commands into stdin. It is tracked separately
  after the container-exec package (JUS-244), which also edits
  `agent/terminal.go`.
* It does not inject the snippet automatically, does not copy it to the
  clipboard without a click, and does not store terminal transcripts or
  credentials.

## Verification

A real-PTY test (`web/tests/terminal-quick-command-pty.test.ts`) starts bash on
a Python `pty`, feeds the snippet via `--rcfile`, waits for a `B` mark, types an
abbreviation, accepts it through the same engine the UI uses, presses Enter and
asserts the command ran exactly once. A negative run starts `sh`/`dash` with no
marks: the scanner stays `unknown`, no suggestion is produced, and ordinary
typing still reaches the shell. The test skips only when `bash`, `sh` or
`python3` is unavailable.
