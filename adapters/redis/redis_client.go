package redis

import "fmt"

type Client struct{}

func NewClient(url string) *Client {
	fmt.Println("Connected to Redis:", url)
	return &Client{}
}
