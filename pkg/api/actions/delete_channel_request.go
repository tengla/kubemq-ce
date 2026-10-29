package actions

import (
	"fmt"
)

type DeleteChannelRequest struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

func NewDeleteChannelRequest() *DeleteChannelRequest {
	return &DeleteChannelRequest{
		Type: "",
		Name: "",
	}
}

func (r *DeleteChannelRequest) ParseRequest(req *Request) error {
	if req.Type != "delete_channel" {
		return fmt.Errorf("request type %s must be %s", req.Type, "delete_channel")
	}

	if req.Data == nil {
		return fmt.Errorf("delete channel request data not found")
	}

	err := transformJsonMapToStruct(req.Data, r)
	if err != nil {
		return fmt.Errorf("error unmarshalling delete channel request data: %s", err.Error())
	}

	return r.Validate()
}

func (r *DeleteChannelRequest) Validate() error {
	if r.Type == "" {
		return fmt.Errorf("type not set")
	}
	if r.Name == "" {
		return fmt.Errorf("name not set")
	}
	return nil
}
