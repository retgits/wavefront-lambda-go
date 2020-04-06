package wflambda

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/rcrowley/go-metrics"
	wavefront "github.com/wavefronthq/go-metrics-wavefront"
)

// incrementCounter increments the counter by the given value if report is true
func incrementCounter(counter metrics.Counter, value int64, report bool) {
	if report {
		counter.Inc(value)
	}
}

// updateGaugeFloat64 increments the counter by the given value if report is true
func updateGaugeFloat64(gauge metrics.GaugeFloat64, value float64, report bool) {
	if report {
		gauge.Update(value)
	}
}

// registerStandardLambdaMetrics creates counters and gauges for the standard AWS Lambda metrics that are reported
// to Wavefront. Whether or not the metrics are actually sent to Wavefront is determined by the environment variable
// REPORT_STANDARD_METRICS.
func registerStandardLambdaMetrics() {
	// Register cold start counter.
	csCounter = metrics.NewCounter()
	csCounterName := wavefront.DeltaCounterName(getStandardLambdaMetricName("coldstarts"))
	wavefront.RegisterMetric(csCounterName, csCounter, nil)

	// Register invocations counter.
	invocationsCounter = metrics.NewCounter()
	invocationsCounterName := wavefront.DeltaCounterName(getStandardLambdaMetricName("invocations"))
	wavefront.RegisterMetric(invocationsCounterName, invocationsCounter, nil)

	// Register Error counter.
	errCounter = metrics.NewCounter()
	errCounterName := wavefront.DeltaCounterName(getStandardLambdaMetricName("errors"))
	wavefront.RegisterMetric(errCounterName, errCounter, nil)

	// Register duration gauge.
	durationGauge = metrics.NewGaugeFloat64()
	wavefront.RegisterMetric(getStandardLambdaMetricName("duration"), durationGauge, nil)

	// Register memTotal counter.
	memTotalGauge = metrics.NewGaugeFloat64()
	wavefront.RegisterMetric(getStandardLambdaMetricName("mem.total"), memTotalGauge, nil)

	// Register memUsed counter.
	memUsedGauge = metrics.NewGaugeFloat64()
	wavefront.RegisterMetric(getStandardLambdaMetricName("mem.used"), memUsedGauge, nil)

	// Register memUsedPercentage counter.
	memUsedPercentageGauge = metrics.NewGaugeFloat64()
	wavefront.RegisterMetric(getStandardLambdaMetricName("mem.percentage"), memUsedPercentageGauge, nil)
}

// reportMetrics sends the collected metrics in the registry to Wavefront. With each metric,
// the point tags listed in the README are sent by the reporter.
func reportMetrics(ctx context.Context) {
	lc, ok := lambdacontext.FromContext(ctx)
	if ok {
		invokedFunctionArn := lc.InvokedFunctionArn
		splitArn := strings.Split(invokedFunctionArn, ":")

		// Expected formats for Lambda ARN are:
		// https://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html#arn-syntax-lambda
		hostTags := map[string]string{
			"LambdaArn":       invokedFunctionArn,
			"source":          lambdacontext.FunctionName,
			"FunctionName":    lambdacontext.FunctionName,
			"ExecutedVersion": lambdacontext.FunctionVersion,
			"Region":          splitArn[3],
			"accountId":       splitArn[4],
		}

		if splitArn[5] == "function" {
			hostTags["Resource"] = splitArn[6]
			if len(splitArn) == 8 {
				hostTags["Resource"] += ":" + splitArn[7]
			}
		} else if splitArn[5] == "event-source-mappings" {
			hostTags["EventSourceMappings"] = splitArn[6]
		}

		err := wavefront.WavefrontOnce(wavefront.WavefrontConfig{
			DirectReporter: wavefront.NewDirectReporter(server, authToken),
			Registry:       metrics.DefaultRegistry,
			DurationUnit:   time.Nanosecond,
			Prefix:         "",
			HostTags:       hostTags,
		})
		if err != nil {
			log.Println("ERROR :: ", err)
		}
	} else {
		log.Println("ERROR :: Couldn't report points to wavefront as retrieving lambdaContext from AWS failed.")
	}
}

// getStandardLambdaMetricName adds the standard Wavefront prefix (aws.lambda.wf) to the name of the metric and returns
// the newly created string.
// for example, getStandardLambdaMetricName("invocations") returns "aws.lambda.wf.invocations"
func getStandardLambdaMetricName(metric string) string {
	const metricPrefix string = "aws.lambda.wf."
	return strings.Join([]string{metricPrefix, metric}, "")
}

// getAndValidateLambdaEnvironment validates whether the required environment variables WAVEFRONT_URL and
// WAVEFRONT_API_TOKEN have been set. If they are not set, the function will panic. The function also checks
// whether the environment variable REPORT_STANDARD_METRICS has been set to false (it will default to true).
// to determine if the standard metrics should be reported.
func getAndValidateLambdaEnvironment() bool {
	server = os.Getenv("WAVEFRONT_URL")
	if server == "" {
		log.Panicf("Environment variable WAVEFRONT_URL is not set.")
	}

	authToken = os.Getenv("WAVEFRONT_API_TOKEN")
	if authToken == "" {
		log.Panicf("Environment variable WAVEFRONT_API_TOKEN is not set.")
	}

	reportStandardLambdaMetrics := os.Getenv("REPORT_STANDARD_METRICS")
	reportStandardMetrics := true
	if reportStandardLambdaMetrics == "False" || reportStandardLambdaMetrics == "false" {
		reportStandardMetrics = false
	}
	return reportStandardMetrics
}
