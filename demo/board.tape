# Browse a busy repo's PR board: instant cached paint, triage preview,
# expanded PR view with checks.
#   go build -o demo/bin/prdash .
#   git clone --depth 1 https://github.com/charmbracelet/bubbletea /tmp/prdash-demo-repo
#   nix shell nixpkgs#vhs -c vhs demo/board.tape
Output demo/board.gif

Set Shell fish
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 14
Set Width 1200
Set Height 640
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms
Set Padding 12

# Hidden warm-up run fills the on-disk cache (isolated XDG_STATE_HOME), so
# the recorded launch shows the stale-while-revalidate instant paint.
Hide
Type "set -gx PATH (pwd)/demo/bin $PATH; set -gx XDG_STATE_HOME /tmp/prdash-demo-state; rm -rf /tmp/prdash-demo-state; cd /tmp/prdash-demo-repo"
Enter
Sleep 1s
Type "prdash"
Enter
Sleep 15s
Type "q"
Sleep 1s
Type "clear"
Enter
Sleep 500ms
Show

Type "prdash"
Enter
Sleep 3s

Type "j"
Sleep 400ms
Type "j"
Sleep 400ms
Type "j"
Sleep 1.5s

Type "l"
Sleep 4s
