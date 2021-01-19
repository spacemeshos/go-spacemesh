Q: Does a proposal message contain an epoch number?

Q: Are delta and K defined in the node config?

Q: What condition triggers the tortoise beacon algorithm? Receiving a certain type (initial) of proposal message

Q: Would something like the below be the correct approach? That's not a full implementation, I will add more to it

- Add an argument `clock TickProvider` to `tortoise.NewTortoise` which receives a `*timesync.TimeClock`
- Add an argument `initProposalMessage <-chan InitProposalMessage` to `tortoise.NewTortoise` which receives a channel of initial proposal messages
- Add an argument `proposalMessages <-chan ProposalMessage` to `tortoise.NewTortoise` which receives a channel of proposal messages 
- Add an argument `k int` to `tortoise.NewTortoise` which receives an amount of rounds `K`
- Add an argument `delta time.Duration` to tortoise.NewTortoise which receives a network delta `δ`
- Clone `hare.Closer` into the tortoise package and embed it into `tortoise.ninjaTortoise`
- Create a method `calculateBeacon` that starts the tortoise beacon algorithm and the ticker. It could look like this:
```go
func (ni *ninjaTortoise) calculateBeacon() {
    <...>
    go tickLoop()
    <...>
}
```

- Create a method tickLoop that fetches initial proposal messages from initProposalMessages in a loop until tortoise.ninjaTortoise is closed. It could look like this:
  
```go
func (ni *ninjaTortoise) tickLoop() {
	for {
		select {
		case message := <-ni.initProposalMessages:
			go ni.handleInitMessage(message)
		case <-ni.CloseChannel():
			return
		}
	}
}
```

- Create a method handleInitMessage that handles initial messages in a new goroutine. It handles K rounds. Something like this:
 
```go
func (ni *ninjaTortoise) handleInitMessage(message InitProposalMessage) {
    ticker := time.NewTicker(ni.delta)
    defer func() { ticker.Stop() } ()
    for i := round; i < ni.k; i++ {
        select {
        case round := <-ticker:
            go ni.handleRound(message, i)
        case <-ni.CloseChannel():
            return
        }
    }
}
``` 