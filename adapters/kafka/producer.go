package kafka

type Producer struct{}

func NewProducer(brokers string) *Producer {
	return &Producer{}
}

func (p *Producer) Publish(topic string, data []byte) error {
	// TODO implement real Kafka producer
	return nil
}
