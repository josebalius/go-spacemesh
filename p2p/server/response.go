package server

import (
	"errors"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type response struct {
	Data []byte
	// use a string instead of an error because error is an interface and cannot be
	// serialized like concrete types.
	ErrorStr string
}

func (r *response) getError() error {
	if len(r.ErrorStr) > 0 {
		return errors.New(r.ErrorStr)
	}
	return nil
}

// SerializeResponse serializes the response data returned by SendRequest
func SerializeResponse(data []byte, err error) []byte {
	resp := response{Data: data}
	if err != nil {
		resp.ErrorStr = err.Error()
	}
	bytes, err := types.InterfaceToBytes(&resp)
	if err != nil {
		log.Panic("failed to serialize response", log.Err(err))
	}
	return bytes
}

// deserializeResponse deserializes the response data returned by SendRequest
func deserializeResponse(data []byte) (*response, error) {
	var resp response
	err := types.BytesToInterface(data, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
