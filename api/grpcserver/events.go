package grpcserver

const subscriptionChanBufSize = 1 << 16

func consumeEvents(inputCh <-chan interface{}) <-chan interface{} {
	outputCh := make(chan interface{}, subscriptionChanBufSize)

	go func(inputCh <-chan interface{}) {
		for event := range inputCh {
			outputCh <- event
		}

		close(outputCh)
	}(inputCh)

	return outputCh
}
