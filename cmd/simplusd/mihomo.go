package main

import "github.com/leonfox28/simplus/internal/mihomosupervisor"

func newMihomoSupervisor(root, socketPath string) (mihomosupervisor.API, error) {
	if socketPath == "" {
		local, err := mihomosupervisor.NewLocal(root)
		if err != nil {
			return nil, err
		}
		return local, nil
	}
	client, err := mihomosupervisor.NewClient(socketPath)
	if err != nil {
		return nil, err
	}
	return client, nil
}
