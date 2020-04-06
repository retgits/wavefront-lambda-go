// Package wflambda is a Go wrapper library for AWS Lambda so you can monitor everything from your Wavefront (https://wavefront.com)
// dashboard. The package includes a set of standard metrics it can send to Wavefront and can be extended to send custom metrics using
// https://github.com/rcrowley/go-metrics.
//
// The reported standard metrics are
//
// | Metric Name                       |  Type         | Description                                                             |
// | --------------------------------- | ------------- | ----------------------------------------------------------------------- |
// | aws.lambda.wf.invocations.count   | Delta Counter | Count of number of lambda function invocations aggregated at the server.|
// | aws.lambda.wf.errors.count        | Delta Counter | Count of number of errors aggregated at the server.                     |
// | aws.lambda.wf.coldstarts.count    | Delta Counter | Count of number of cold starts aggregated at the server.                |
// | aws.lambda.wf.duration.value      | Gauge         | Execution time of the Lambda handler function in milliseconds.          |
//
// To connect to Wavefront, you'll need to set the WAVEFRONT_URL and WAVEFRONT_API_TOKEN environment variables. To send the above
// standard metrics, you'll need to set the environment variable REPORT_STANDARD_METRICS to true.
package wflambda

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/rcrowley/go-metrics"
)

type lambdaHandlerFunction func(context.Context, json.RawMessage) (interface{}, error)

var (
	server                    string
	authToken                 string
	reportStandardMetrics     bool
	lambdaHandlerTakesContext bool
	handlerType               reflect.Type
	handlerValue              reflect.Value
	coldStart                 = true
	csCounter                 metrics.Counter
	invocationsCounter        metrics.Counter
	errCounter                metrics.Counter
	memTotalGauge             metrics.GaugeFloat64
	memUsedGauge              metrics.GaugeFloat64
	memUsedPercentageGauge    metrics.GaugeFloat64
	durationGauge             metrics.GaugeFloat64
)

// Wrapper returns the Wavefront Lambda wrapper. The wrapper collects the AWS Lambda standard metrics and reports it directly to
// the specified Wavefront URL. To successfully execute the Lambda function and send metrics to Wavefront, the following
// environment variables should be set:
//
// * WAVEFRONT_URL: The URL of your Wavefront instance (like https://myinstance.wavefront.com).
// * WAVEFRONT_API_TOKEN: Your Wavefront API token (see the [docs](https://docs.wavefront.com/wavefront_api.html) how to create an API token).
// * REPORT_STANDARD_METRICS: Report standard metrics or not (defaults to true).
func Wrapper(lambdaHandler interface{}) interface{} {
	// Validate wrapper environment variables.
	reportStandardMetrics = getAndValidateLambdaEnvironment()

	// Check if lambdaHandler is a valid handler.
	handlerTakesContext, err := validateLambdaHandler(lambdaHandler)
	lambdaHandlerTakesContext = handlerTakesContext
	if err != nil {
		return lambdaErrorHandler(err)
	}
	handlerType = reflect.TypeOf(lambdaHandler)
	handlerValue = reflect.ValueOf(lambdaHandler)

	// Returns a wrapper function with standard Lambda metrics.
	return lambdaHandlerWrapper
}

// lambdaHandlerWrapper wraps the invocation of the actual AWS Lambda function to collect metrics that can be reported back to Wavefront.
func lambdaHandlerWrapper(ctx context.Context, payload json.RawMessage) (response interface{}, lambdaHandlerError error) {
	defer func() {
		var err interface{}
		// Increment error count if there is a panic or non nil error is returned
		// by users lambda handler function.
		if e := recover(); e != nil {
			err = e
			// Set error counters
			incrementCounter(errCounter, 1, reportStandardMetrics)
		} else if lambdaHandlerError != nil {
			// Set error counters
			incrementCounter(errCounter, 1, reportStandardMetrics)
		}
		reportMetrics(ctx)
		metrics.DefaultRegistry.UnregisterAll()
		if err != nil {
			panic(err)
		}
	}()

	var args []reflect.Value
	if lambdaHandlerTakesContext {
		args = append(args, reflect.ValueOf(ctx))
	}
	if (handlerType.NumIn() == 1 && !lambdaHandlerTakesContext) || handlerType.NumIn() == 2 {
		inputParamType := handlerType.In(handlerType.NumIn() - 1)
		paramValue := reflect.New(inputParamType)
		if e := json.Unmarshal(payload, paramValue.Interface()); e != nil {
			return nil, e
		}
		elem := paramValue.Elem()
		args = append(args, elem)
	}
	if reportStandardMetrics {
		registerStandardLambdaMetrics()
	}

	if coldStart {
		// Set cold start counter.
		incrementCounter(csCounter, 1, reportStandardMetrics)
		coldStart = false
	}
	// Set invocations counter.
	incrementCounter(invocationsCounter, 1, reportStandardMetrics)
	start := time.Now()
	lambdaResponse := handlerValue.Call(args)
	// set memory usage
	memstats := getMemoryStats()
	updateGaugeFloat64(memTotalGauge, memstats.Total, reportStandardMetrics)
	updateGaugeFloat64(memUsedGauge, memstats.Used, reportStandardMetrics)
	updateGaugeFloat64(memUsedPercentageGauge, memstats.UsedPercentage, reportStandardMetrics)
	executionDuration := time.Since(start)
	// Set duration gauge value in milliseconds.
	updateGaugeFloat64(durationGauge, executionDuration.Seconds()*1000, reportStandardMetrics)
	if len(lambdaResponse) == 0 {
		return nil, nil
	}
	var err error
	if len(lambdaResponse) > 0 {
		// The last value must always implement error.
		if e, ok := lambdaResponse[len(lambdaResponse)-1].Interface().(error); ok {
			err = e
		}
	}
	var val interface{}
	if len(lambdaResponse) == 2 {
		// In case lambda handler returns 2 arguments(i.e. Maximum allowed return arguments), first
		// argument represents a valid non-error value compatible with the encoding/json standard library.
		val = lambdaResponse[0].Interface()
	}

	return val, err
}

// lambdaErrorHandler returns a lambdaHandlerFunction to report an error in case the lambdaHandler is not a valid lambdaHandler
func lambdaErrorHandler(e error) lambdaHandlerFunction {
	return func(ctx context.Context, event json.RawMessage) (interface{}, error) {
		return nil, e
	}
}
