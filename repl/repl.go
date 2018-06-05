package repl

import (
	"fmt"
	"strings"

	"github.com/c-bata/go-prompt"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

const (
	prefix      = "$ "
	printPrefix = "> "
)

var emptyComplete = func(prompt.Document) []prompt.Suggest { return []prompt.Suggest{} }

type command struct {
	text        string
	description string
	fn          func()
}

type repl struct {
	commands []command
	node     p2p.LocalNode
	input    string
}

func StartRepl(node p2p.LocalNode, envPath string) {
	err := setVariables(envPath)
	if err != nil {
		node.Debug(err.Error())
	}

	r := &repl{node: node}
	r.initializeCommands()

	p := prompt.New(
		r.executor,
		r.completer,
		prompt.OptionPrefix(prefix),
		prompt.OptionPrefixTextColor(prompt.LightGray),
		prompt.OptionMaxSuggestion(uint16(len(r.commands))),
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
		{"transfer coins", "Transfer coins between 2 accounts.", r.transferCoins},
		{"modify", "Modify any CLI flag or param.", r.modifyCLIFlagOrParam},
		{"restart node", "Restart node.", r.restartNode},
		{"setup", "Setup.", r.setup},
		{"echo", "Echo runtime variable.", r.echoVariable},
	}
}

func (r *repl) executor(text string) {
	for _, c := range r.commands {
		if len(text) >= len(c.text) && text[:len(c.text)] == c.text {
			r.input = text
			r.node.Debug(userExecutingCommandMsg, c.text)
			c.fn()
			return
		}
	}

	fmt.Println(printPrefix, "invalid command.")
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
	createNewAccount := r.yesOrNoQuestion(welcomeMsg) == "y"
	if createNewAccount {
		r.createAccount()
	}
}

func (*repl) yesOrNoQuestion(msg string) string {
	var input string
	for {
		input = prompt.Input(prefix+msg,
			emptyComplete,
			prompt.OptionPrefixTextColor(prompt.LightGray))

		if input == "y" || input == "n" {
			break
		}

		fmt.Println(printPrefix, "invalid command.")
	}

	return input
}

func (r *repl) createAccount() {
	generatePassphrase := r.yesOrNoQuestion(generateMsg) == "y"
	accountInfo := prompt.Input(prefix+accountInfoMsg,
		emptyComplete,
		prompt.OptionPrefixTextColor(prompt.LightGray))

	// TODO: call function to create account
	fmt.Println(generatePassphrase, accountInfo)
}

func (r *repl) unlockAccount() {
	passphrase := r.commandLineParams(1, r.input)

	// TODO: call function to unlock account
	fmt.Println("unlockAccount", passphrase)
}

func (r *repl) commandLineParams(idx int, input string) string {
	c := r.commands[idx]
	params := strings.Replace(input, c.text, "", -1)

	return strings.TrimSpace(params)
}

func (r *repl) lockAccount() {
	passphrase := r.commandLineParams(2, r.input)

	// TODO: call function to lock account
	fmt.Println("lockAccount", passphrase)
}

func (r *repl) account() {
	accountCommand := r.commands[3]
	accountAddress := strings.Replace(r.input, accountCommand.text, "", -1)

	if accountAddress != "" {
		// TODO: call account info function
	} else {
		createAccount := r.yesOrNoQuestion(accountNotFoundoMsg) == "y"
		if createAccount {
			r.createAccount()
		}

		// TODO: call account info function
	}

	fmt.Println("account", r.input)
}

func (r *repl) transferCoins() {
	var accountId string
	fmt.Println(printPrefix, initialTransferMsg)
	isTransferFromLocal := r.yesOrNoQuestion(transferFromLocalAccountMsg) == "y"

	if !isTransferFromLocal {
		accountId = r.inputNotBlank(transferFromAccountMsg)
	} else {
		// TODO: call function to get the address
		accountId = "bla bla bla"
	}

	destinationAccountId := r.inputNotBlank(transferToAccountMsg)
	amount := r.inputNotBlank(amountToTransferMsg)

	// TODO: check if the account is lock

	fmt.Println(printPrefix, "Transaction summary:")
	fmt.Println(printPrefix, "From:", accountId)
	fmt.Println(printPrefix, "To:", destinationAccountId)
	fmt.Println(printPrefix, "Amount:", amount)

	if r.yesOrNoQuestion(confirmTransactionMsg) == "y" {
		// TODO: call transfer function
	}

	fmt.Println(r.input)
}

func (*repl) inputNotBlank(msg string) string {
	var input string
	for {
		input = prompt.Input(prefix+msg,
			emptyComplete,
			prompt.OptionPrefixTextColor(prompt.LightGray))

		if strings.TrimSpace(input) != "" {
			break
		}

		fmt.Println(printPrefix, "please enter a value.")
	}

	return input
}

func (r *repl) modifyCLIFlagOrParam() {
	fmt.Println(r.input)
}

func (r *repl) restartNode() {
	flagsAndParams := prompt.Input(prefix+newFlagsAndParamsMsg,
		emptyComplete,
		prompt.OptionPrefixTextColor(prompt.LightGray))

	fmt.Println(r.input, flagsAndParams)
}

func (r *repl) setup() {

	// TODO: call setup function
	fmt.Println(r.input)
}

func (r *repl) echoVariable() {
	echoKey := r.commandLineParams(8, r.input)

	echo(echoKey)
}
