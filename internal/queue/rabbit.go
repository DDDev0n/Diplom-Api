package queue

import (
	"context"
	"encoding/json"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const paymentQueue = "payments.process"

type PaymentJob struct {
	PaymentID int64 `json:"payment_id"`
}

type Publisher struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

func NewPublisher(url string) (*Publisher, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := declareQueue(ch); err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}
	return &Publisher{conn: conn, ch: ch}, nil
}

func (p *Publisher) PublishPayment(ctx context.Context, paymentID int64) error {
	body, err := json.Marshal(PaymentJob{PaymentID: paymentID})
	if err != nil {
		return err
	}
	return p.ch.PublishWithContext(ctx, "", paymentQueue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}

func (p *Publisher) Close() {
	p.ch.Close()
	p.conn.Close()
}

type Consumer struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

func NewConsumer(url string) (*Consumer, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := declareQueue(ch); err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}
	return &Consumer{conn: conn, ch: ch}, nil
}

func (c *Consumer) ConsumePayments(ctx context.Context, handle func(context.Context, PaymentJob) error) error {
	deliveries, err := c.ch.Consume(paymentQueue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case delivery, ok := <-deliveries:
			if !ok {
				return nil
			}
			var job PaymentJob
			if err := json.Unmarshal(delivery.Body, &job); err != nil {
				delivery.Nack(false, false)
				continue
			}
			if err := handle(ctx, job); err != nil {
				time.Sleep(2 * time.Second)
				delivery.Nack(false, true)
				continue
			}
			delivery.Ack(false)
		}
	}
}

func (c *Consumer) Close() {
	c.ch.Close()
	c.conn.Close()
}

func declareQueue(ch *amqp.Channel) error {
	_, err := ch.QueueDeclare(paymentQueue, true, false, false, false, nil)
	return err
}
