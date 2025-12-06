package processing

type Dispatcher struct {
	// future: route jobs to appropriate processor
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{}
}

func (d *Dispatcher) Process(jobType string, payload interface{}) error {
	// route to correct processor
	return nil
}
