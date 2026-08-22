# Left terminal of the chat scene: a Sonnet agent that joins the demo room,
# greets agent-b, then answers whatever is addressed to it. Spawned by
# run.mjs with DEMO_BIN / DEMO_ROOT in the environment; the Hide block
# consumes them. Runs ~70s; run.mjs trims all three chat recordings to the
# shortest. Output path comes from vhs -o.
#
# Standalone: DEMO_BIN=bin/atomic DEMO_ROOT=~/projects/noorm vhs marketing/demo/tapes/agent-a.tape -o /tmp/agent-a.mp4


Set Shell         "zsh"
Set FontFamily    "JetBrains Mono"
Set FontSize      15
Set Width         640
Set Height        540
Set Padding       14
Set Theme         "Catppuccin Mocha"
Set WindowBar     Colorful
Set BorderRadius  8
Set TypingSpeed   35ms
Set Framerate     30

Hide
Type `export PATH="$(dirname "$DEMO_BIN"):$PATH"; cd "$DEMO_ROOT"; unset ${(M)${(k)parameters}:#CLAUDE*}; export PROMPT='%F{green}agent-a ❯%f '; clear`
Enter
Show

Sleep 600ms
Type `claude --model sonnet --strict-mcp-config --allowedTools "Bash(atomic bus:*),Monitor"`
Sleep 400ms
Enter
Sleep 5s
Type `Join bus room atomic-demo as agent-a. Run atomic bus who atomic-demo, then send a short hello addressed to the member whose name ends in agent-b (wait for it to appear if needed). Then watch the room for about 45 seconds and reply briefly to anything addressed to you. No tool use beyond atomic bus.`
Sleep 600ms
Enter
Sleep 62s
Ctrl+C
Sleep 300ms
Ctrl+C
Sleep 1s
