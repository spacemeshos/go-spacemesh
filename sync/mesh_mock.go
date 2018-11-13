package sync

type LayersMocks struct {
	layerCount       int64
	latestKnownLayer int64
	layers           []LayerMock
}

func NewMockLayers() Mesh {
	return &LayersMocks{0, 0, make([]LayerMock, 100)}
}

func (lm *LayersMocks) GetLayer(i int) (Layer, error) {
	return &lm.layers[i-1], nil
}

func (lm *LayersMocks) Close() {

}

func (lm *LayersMocks) GetBlock(layer int, id []byte) (Block, error) {
	l := lm.layers[layer-1]
	return l.blocks[0], nil
}

func (lm *LayersMocks) LocalLayerCount() int {
	return int(lm.layerCount)
}

func (lm *LayersMocks) LatestKnownLayer() int {
	return int(lm.latestKnownLayer)
}

func (lm *LayersMocks) SetLatestKnownLayer(idx int) {
	lm.latestKnownLayer = int64(idx)
}

func (lm *LayersMocks) AddLayer(idx int, layer []Block) error {
	lm.layers = append(lm.layers, LayerMock{idx, layer})
	lm.layerCount++
	return nil
}
