package app

// bashDeleteCurrentHistoryEntry removes the running bootstrap on both current
// Bash and the Bash 3.2 shipped by macOS. Bash 3 can expose the next history
// position through HISTCMD while executing a line, so it uses the prior index.
const bashDeleteCurrentHistoryEntry = `history -d "$((HISTCMD-(BASH_VERSINFO[0]<4)))"`

// bashDeleteTokenHistoryEntry removes only a command containing the fresh token's
// literal split spelling. Ignored commands and disabled history leave an older
// user entry at HISTCMD; that entry must survive. Non-Bash shells do nothing.
func bashDeleteTokenHistoryEntry(token string) string {
	splitToken := quoteSplitTerminalCommandToken(token)
	return "if [ -n \"${BASH_VERSION-}\" ];then case \"$(HISTTIMEFORMAT= builtin history 1)\" in *" + quoteTerminalCommand(splitToken) + "*) builtin " + bashDeleteCurrentHistoryEntry + ";;esac;fi;"
}
