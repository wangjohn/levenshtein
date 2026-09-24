package consumer

func Load() int { return 1 }

func Old() int {
	//lint:ignore faulty_fine the old name
	return 2
}
