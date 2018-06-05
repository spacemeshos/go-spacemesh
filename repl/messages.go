package repl

const (
	welcomeMsg                  = "Welcome to Spacemesh. To get started you need a new local account. Create a new local user account now? (y/n) "
	generateMsg                 = "Generate account passphrase? (y/n) "
	accountInfoMsg              = "Add account info (enter text or ENTER):"
	accountNotFoundoMsg         = "Local account not found. Create one? (y/n) "
	initialTransferMsg          = "Transfer coin from local account to another account."
	transferFromLocalAccountMsg = "Transfer from local account %s ? (y/n) "
	transferFromAccountMsg      = "Enter or paste account id: "
	transferToAccountMsg        = "Enter or paste destination account id: "
	amountToTransferMsg         = "Enter Meshcoins amount to transfer: "
	accountPassphrase           = "Enter local account passphrase: "
	confirmTransactionMsg       = "Confirm transaction (y/n): "
	newFlagsAndParamsMsg        = "provide CLI flags and params or press ENTER for none: "
	userExecutingCommandMsg     = "User executing command: %s"
	requiresSetupMsg            = "Spacemesh requires a minimum of 300GB of free disk space. 250GB are used for POST and 50GB are reserved for the global computer state. You may allocate additional disk space for POST in 300GB increments. "
	postAllocationMsg           = "POST allocation (GB): "
	restartNodeMsg              = "Restart node?"
)
