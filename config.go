package main

type Config struct {
	Env             string `envconfig:"ENV"`
	Port            int    `required:"true"`
	TracingEndpoint string `required:"true" split_words:"true"`
	MetricsEndpoint string `required:"true" split_words:"true"`
}
