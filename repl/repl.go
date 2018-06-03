package repl

import (
	"github.com/c-bata/go-prompt"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

const prefix = "$ "

type command struct {
	text        string
	description string
	fn          func()
}

type repl struct {
	commands []command
	node     p2p.LocalNode
}

func StartRepl(node p2p.LocalNode) {
	r := &repl{node: node}
	r.initializeCommands()

	p := prompt.New(
		r.executor,
		r.completer,
		prompt.OptionPrefix(prefix),
		prompt.OptionPrefixTextColor(prompt.LightGray),
	)

	r.firstTime()
	p.Run()
}

func (r *repl) initializeCommands() {
	r.commands = []command{
		{"create account", "Create account.", r.createAccount},
		{"unlock account", "Unlock account.", r.unlockAccount},
		{"lock account", "Lock Account.", r.lockAccount},
		{"account", "Shows basic local account info about the local account.", r.account},
		{"transfer coins", "Get Account Info.", r.accountInfo},
		{"transfer", "Transfer coins between 2 accounts.", r.transferCoins},
		{"modify", "Modify any CLI flag or param.", r.modifyCLIFlagOrParam},
		{"rrestart node", "Restart node.", r.restartNode},
		{"setup", "Setup.", r.setup},
	}
}

func (r *repl) executor(text string) {
	for _, c := range r.commands {
		if text == c.text {
			c.fn()
			return
		}
	}

	r.node.Info("%s%s", prefix, "invalid command.")
}

func (r *repl) completer(in prompt.Document) []prompt.Suggest {
	suggets := make([]prompt.Suggest, 0)
	for _, command := range r.commands {
		s := prompt.Suggest{
			Text:        command.text,
			Description: command.description,
		}

		suggets = append(suggets, s)
	}

	return prompt.FilterHasPrefix(suggets, in.GetWordBeforeCursor(), true)
}

func (r *repl) firstTime() {
	//welcome := "Welcome to Spacemesh. To get started you need a new local account. Create a new local user account now? (y/n)"
	//generate := "Generate account passphrase (y/n)?"
	//info := "Add account info (enter text or ENTER):Avive main Spacemesh account"
}

func (r *repl) createAccount() {
}

func (r *repl) unlockAccount() {

}

func (r *repl) account() {

}

func (r *repl) lockAccount() {

}

func (r *repl) accountInfo() {

}

func (r *repl) transferCoins() {

}

func (r *repl) modifyCLIFlagOrParam() {

}

func (r *repl) restartNode() {

}

func (r *repl) setup() {

}
