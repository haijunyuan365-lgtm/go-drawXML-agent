package logging

import "go.uber.org/zap"

func New(env string) (*zap.Logger, error) {
	if env == "prod" || env == "production" {
		return zap.NewProduction()
	}

	return zap.NewDevelopment()
}
