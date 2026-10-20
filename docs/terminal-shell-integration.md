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
Alternate-screen switches (`?1049h`, `?1047h`, `?47h`, including semicolon
lists such as `?25;1049h`) disable suggestions until the matching `l`.

The local mirror is intentionally narrow. It advances only on one printable
ASCII character or Backspace from a `B`-marked fresh prompt. **Everything else
suspends matching until the next `B`**: Enter, Esc (vi normal mode / readline
meta), Ctrl-R/Ctrl-S search, Ctrl-C, Tab completion, history recall, cursor
keys/Home/End, paste, IME composition, and any other control byte. A reset
mirror can therefore never pass the word-boundary check for text the client
did not type (`foogs` does not match `gs`, even after Up/Tab/Ctrl-E).

Accepting sends **Backspace × the typed abbreviation length** and then the
command text. It never sends Enter. The count is safe because the mirror
proves the abbreviation is the last text before the cursor. In bash/zsh **vi
normal mode** the mirror is suspended after the Esc that leaves insert mode, so
no suggestion can appear and no readline/vi command is injected.

## Keyboard behaviour

The shell keeps its keys. **Esc, Ctrl+Space, Tab and Enter are forwarded to the
PTY** unless the suggestion list is explicitly focused.

| Chord | Effect |
|---|---|
| type the abbreviation | the strip appears above the terminal viewport (never over the prompt line) |
| `Ctrl+Shift+Space` | focus the visible suggestion list |
| `Enter` (list focused) | insert the command, without running it |
| `Tab` (list focused) | return focus to the terminal, list stays open |
| `Esc` (list focused) | dismiss the suggestion and return focus |
| `Esc` while typing | forwarded to the shell; the suggestion is dismissed and matching pauses until the next prompt |

The strip is a valid `listbox` owning one `option`, with a `role="status"`
live announcement and a **Choose** button for pointer/screen-reader access.
The option's accessible name is its visible text (abbreviation, label,
command).

## Supported shells

| Shell | Status | Notes |
|---|---|---|
| bash ≥ 4.4 | **Supported, PTY-tested** | `PS0` emits the `C` mark. Older bash still emits A/B/D through `PROMPT_COMMAND`; Enter suspends matching, so the absence of `C` is safe, only less precise. |
| zsh ≥ 5.3 | **Supported, PTY-tested** | `add-zsh-hook precmd/preexec`; the snippet adds the `B` mark to `PROMPT` once and re-checks every prompt so themes that rebuild `PROMPT` recover on the next prompt. |
| fish | **Not supported in JUS-249** | No tested integration is shipped; suggestions stay off, manual insertion still works. |
| sh / dash | **Not supported** | No prompt hook or `PS0`; suggestions stay off, manual insertion still works. |
| ksh / others | **Not supported** | Same as sh/dash. |

"Not supported" is a clean state, not an error: the terminal keeps working,
typing is forwarded unchanged, and the quick-command panel shows the setup
hint without offering a snippet for an untested shell.

## bash setup (additive, optional)

Append to `~/.bashrc` (or the file passed with `--rcfile`). It keeps the
existing prompt and `PROMPT_COMMAND`, and re-adds the `B` mark if a theme
rewrote `PS1`. Bash 5.1+ allows `PROMPT_COMMAND` to be an array; if you use an
array, call `__gotham_prompt_command` as its first element instead of the
string assignment below.

```bash
# ~/.bashrc — optional Gotham prompt marks (bash >= 4.4).
__gotham_prompt_command() {
  local st=$?
  printf '\033]133;D;%s\a' "$st"
  printf '\033]133;A\a'
  case "$PS1" in
    *'133;B'*) ;;
    *) PS1="$PS1"'\[\033]133;B\a\]' ;;
  esac
  return $st
}
PROMPT_COMMAND="__gotham_prompt_command${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
PS0="\[\033]133;C\a\]${PS0:-}"
```

The `B` mark is appended by `PROMPT_COMMAND` before each prompt. If a theme's
own `PROMPT_COMMAND` runs **after** this one and replaces `PS1`, the mark is
re-added on the following prompt; put `__gotham_prompt_command` last in that
case.

## zsh setup (additive, optional)

Append to `~/.zshrc` after your theme/hook setup. `status` is a read-only
special parameter in zsh, so the snippet uses `st`. `add-zsh-hook` appends the
function to the existing hook list, and the `B` mark is added to `PROMPT` once
(guarded) so repeated prompts do not accumulate escape bytes.

```zsh
# ~/.zshrc — optional Gotham prompt marks (zsh >= 5.3).
__gotham_precmd() {
  local st=$?
  print -n $'\e]133;D;'"$st"$'\a\e]133;A\a'
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

If your theme recomputes `PROMPT` in its own `precmd` after this hook, append
the `B` mark in that theme's function instead; the guard means it is never
duplicated.

## Stored commands

- Up to **40** commands per account, label ≤ 64, text ≤ 2,048, abbreviation ≤
  24 (letters/digits/dot/dash/underscore, starting alphanumeric).
- Stored in this browser's `localStorage` under a per-account key, versioned
  and schema-validated. A corrupt/oversized payload reads as an empty list and
  a blocked/quota write keeps the in-memory list and shows a warning.
- The payload guard is 512 KiB, enough for the worst allowed list after JSON
  escaping. Control characters other than newline/tab and Unicode bidi control
  characters are rejected at **save** time (and again at insert time for
  legacy data). Multiline/tab text is inserted only inside bracketed paste
  when the live terminal reports mode 2004; otherwise it is refused.
- Commands are ordinary storage, **not encrypted**. The panel says not to save
  secrets. No transcript, prompt text or credential is stored.

## What this build does not do

* It does not start the shell with an integration rcfile. Starting bash with
  `--rcfile` (and zsh with an additive `ZDOTDIR` rc) belongs to the node agent,
  which currently runs `exec.Command(shell)` with no arguments. That follow-up
  (JUS-252) must be additive and optional: source the operator's normal rc
  first, add the marks last, and never inject commands into stdin.
* It does not inject the snippet automatically, does not copy it to the
  clipboard without a click, and does not store terminal transcripts or
  credentials.

## Verification

`web/tests/terminal-quick-command-pty.test.ts` runs real PTYs and the production
scanner/engine. It skips a suite with a console warning only when the shell is
absent:

| Case | Assertion |
|---|---|
| bash happy path | `gs` → command, one run, no duplicated prefix |
| bash vi mode | Esc (normal mode) → no suggestion; fresh insert prompt → replacement runs once, no `echoecho` |
| Ctrl-R | no suggestion inside reverse search; fresh prompt recovers |
| history/Tab/Ctrl-E/`foogs` | no false match after a reset; fresh prompt recovers |
| PS2 | continuation has no `B`; no suggestion; next prompt recovers |
| password prompt | `read -s` has no `B`; no suggestion; next prompt recovers |
| alternate screen | `?1049h` disables; `?1049l` + prompt restores |
| failing rc file | no marks, no suggestion, shell still runs commands |
| zsh | marks emitted, no `read-only variable` error, replacement runs once |
| sh/dash | no marks, no suggestion, ordinary typing works |

The zsh run sets `ZDOTDIR` to a temp directory so the operator's own config is
not involved. Bash on macOS 3.2 is exercised too (A/B/D via `PROMPT_COMMAND`);
the `C`/`PS0` path is exercised on the Linux CI runner where bash is 5.x.
