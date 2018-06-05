package repl

import (
	"fmt"
	"strings"

	"github.com/c-bata/go-prompt"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

const (
	prefix      = "$ "
	printPrefix = ">"
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

// StartRepl starts REPL.
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
		{"setup", "Setup POST.", r.setup},
		{"restart node", "Restart node.", r.restartNode},
		{"set", "change CLI flag or param. E.g. set param a=5 flag c=5 or E.g. set param a=5", r.setCLIFlagOrParam},
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

// executes prompt waiting for an input with y or n
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

	err := r.node.CreateAccount(generatePassphrase, accountInfo)
	if err != nil {
		r.node.Debug(err.Error())
	} else {
		r.setup()
	}
}

func (r *repl) unlockAccount() {
	passphrase := r.commandLineParams(1, r.input)
	err := r.node.Unlock(passphrase)
	if err != nil {
		r.node.Debug(err.Error())
	} else {
		acctCmd := r.commands[3]
		r.executor(fmt.Sprintf("%s %s", acctCmd.text, passphrase))
	}
}

func (r *repl) commandLineParams(idx int, input string) string {
	c := r.commands[idx]
	params := strings.Replace(input, c.text, "", -1)

	return strings.TrimSpace(params)
}

func (r *repl) lockAccount() {
	passphrase := r.commandLineParams(2, r.input)
	err := r.node.Lock(passphrase)
	if err != nil {
		r.node.Debug(err.Error())
	} else {
		acctCmd := r.commands[3]
		r.executor(fmt.Sprintf("%s %s", acctCmd.text, passphrase))
	}
}

func (r *repl) account() {
	accountId := r.commandLineParams(3, r.input)

	if accountId != "" {
		r.node.AccountInfo(accountId)
	} else {
		if acct := r.node.LocalAccount(); acct == nil &&
			r.yesOrNoQuestion(accountNotFoundoMsg) == "y" {
			r.createAccount()
		} else {
			r.node.AccountInfo(accountId)
		}
	}
}

func (r *repl) transferCoins() {
	accountId := ""
	passphrase := ""

	fmt.Println(printPrefix, initialTransferMsg)

	acct := r.node.LocalAccount()
	if acct == nil {
		accountCommand := r.commands[3]

		// executing account command to create a local account
		r.executor(accountCommand.text)
		return
	}

	accountId = acct.PrivKey.String()
	msg := fmt.Sprintf(transferFromLocalAccountMsg, accountId)
	isTransferFromLocal := r.yesOrNoQuestion(msg) == "y"

	if !isTransferFromLocal {
		accountId = r.inputNotBlank(transferFromAccountMsg)
	}

	destinationAccountId := r.inputNotBlank(transferToAccountMsg)
	amount := r.inputNotBlank(amountToTransferMsg)

	if !r.node.IsAccountUnLock(accountId) {
		passphrase = r.inputNotBlank(accountPassphrase)
	}

	fmt.Println(printPrefix, "Transaction summary:")
	fmt.Println(printPrefix, "From:", accountId)
	fmt.Println(printPrefix, "To:", destinationAccountId)
	fmt.Println(printPrefix, "Amount:", amount)

	if r.yesOrNoQuestion(confirmTransactionMsg) == "y" {
		err := r.node.Transfer(accountId, destinationAccountId, amount, passphrase)
		if err != nil {
			r.node.Debug(err.Error())
		}
	}
}

// executes prompt waiting an input not blank
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

func (r *repl) setCLIFlagOrParam() {
	var err error
	params, flags := r.getParamsAndFlags(r.commandLineParams(5, r.input))

	if r.node.NeedRestartNode(params, flags) {
		if r.yesOrNoQuestion(restartNodeMsg) == "y" {
			err = r.node.Restart(params, flags)
		}
	} else {
		err = r.node.SetVariables(params, flags)

	}

	if err != nil {
		r.node.Debug(err.Error())
	}
}

func (*repl) getParamsAndFlags(input string) ([]string, []string) {
	values := strings.Split(input, " ")
	updateParams := false
	params := make([]string, 0)
	flags := make([]string, 0)

	for _, v := range values {
		if v == "param" {
			updateParams = true
			continue
		}

		if v == "flag" {
			updateParams = false
			continue
		}

		if updateParams {
			params = append(params, v)
		} else {
			flags = append(flags, v)
		}
	}

	return params, flags
}

func (r *repl) restartNode() {
	flagsAndParams := prompt.Input(prefix+newFlagsAndParamsMsg,
		emptyComplete,
		prompt.OptionPrefixTextColor(prompt.LightGray))

	params, flags := r.getParamsAndFlags(flagsAndParams)

	err := r.node.Restart(params, flags)
	if err != nil {
		r.node.Debug(err.Error())
	}
}

func (r *repl) setup() {
	allocation := r.inputNotBlank(postAllocationMsg)

	err := r.node.Setup(allocation)
	if err != nil {
		r.node.Debug(err.Error())
	}
}

func (r *repl) echoVariable() {
	echoKey := r.commandLineParams(8, r.input)

	echo(echoKey)
}
