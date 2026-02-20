# VHS Tape - Hookwise Hero Demo
# Records: init → doctor → guard demo

Output screenshots/demo.gif
Set FontSize 14
Set Width 900
Set Height 600
Set Theme "Dracula"
Set Padding 20
Set TypingSpeed 40ms

# Title
Type "# hookwise — guard rails for Claude Code"
Enter
Sleep 1s

# Step 1: Init
Type "hookwise init --preset minimal"
Enter
Sleep 2s

# Step 2: Doctor check
Type "hookwise doctor"
Enter
Sleep 3s

# Step 3: Show the config
Type "cat hookwise.yaml"
Enter
Sleep 3s

# Step 4: Simulate a guard block
Type "echo '⛔ BLOCKED by hookwise guard'"
Enter
Sleep 500ms
Type "echo '  Tool: Bash | Command: rm -rf / | Action: block'"
Enter
Sleep 500ms
Type "echo '  Rule: Dangerous command blocked (first-match-wins)'"
Enter
Sleep 2s

# End
Type "# Your AI agent, safely guarded. 🛡️"
Enter
Sleep 2s
