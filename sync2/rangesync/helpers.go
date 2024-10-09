package rangesync

// CollectSetItems returns the list of items in the given set.
func CollectSetItems(os OrderedSet) (r []KeyBytes, err error) {
	items := os.Items()
	var first KeyBytes
	for v := range items.Seq {
		if first == nil {
			first = v
		} else if v.Compare(first) == 0 {
			break
		}
		r = append(r, v)
	}
	return r, items.Error()
}
