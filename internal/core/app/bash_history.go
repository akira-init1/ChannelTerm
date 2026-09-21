package app

// bashDeleteCurrentHistoryEntry removes the running bootstrap on both current
// Bash and the Bash 3.2 shipped by macOS. Bash 3 can expose the next history
// position through HISTCMD while executing a line, so it uses the prior index.
const bashDeleteCurrentHistoryEntry = `history -d "$((HISTCMD-(BASH_VERSINFO[0]<4)))"`
