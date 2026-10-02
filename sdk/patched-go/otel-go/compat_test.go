package otel_test

import (
	"context"
	"testing"

	otel "github.com/dagger/otel-go"
	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestLogValuesRoundTrip(t *testing.T) {
	values := []attribute.Value{
		attribute.BoolValue(true),
		attribute.Int64Value(42),
		attribute.Float64Value(1.5),
		attribute.StringValue("log body"),
		attribute.ByteSliceValue([]byte{0, 127, 255}),
		attribute.SliceValue(attribute.StringValue("mixed"), attribute.Int64Value(9)),
		attribute.MapValue(attribute.KeyValue{Key: "nested", Value: attribute.SliceValue(attribute.BoolValue(true))}),
		attribute.BoolSliceValue([]bool{true, false}),
		attribute.Int64SliceValue([]int64{1, 2}),
		attribute.Float64SliceValue([]float64{1.5, 2.5}),
		attribute.StringSliceValue([]string{"a", "b"}),
	}

	for _, value := range values {
		t.Run(value.Type().String(), func(t *testing.T) {
			encoded := otel.LogValueToPB(value)
			decoded := otel.LogValueFromPB(encoded)

			if !proto.Equal(otel.LogValueToPB(decoded), encoded) {
				t.Fatalf("value changed during protobuf round trip: %v", value)
			}
			if encoded.GetStringValue() == "INVALID" {
				t.Fatalf("value was not encoded: %v", value)
			}
		})
	}
}

func TestLogHomogeneousSliceEncoding(t *testing.T) {
	values := []struct {
		homogeneous attribute.Value
		mixed       attribute.Value
	}{
		{attribute.BoolSliceValue([]bool{true, false}), attribute.SliceValue(attribute.BoolValue(true), attribute.BoolValue(false))},
		{attribute.Int64SliceValue([]int64{1, 2}), attribute.SliceValue(attribute.Int64Value(1), attribute.Int64Value(2))},
		{attribute.Float64SliceValue([]float64{1.5}), attribute.SliceValue(attribute.Float64Value(1.5))},
		{attribute.StringSliceValue([]string{"a"}), attribute.SliceValue(attribute.StringValue("a"))},
	}

	for _, value := range values {
		if !proto.Equal(otel.LogValueToPB(value.homogeneous), otel.LogValueToPB(value.mixed)) {
			t.Fatalf("homogeneous slice differs from equivalent mixed slice: %v", value.homogeneous)
		}
	}
}

type logCollector struct {
	records []sdklog.Record
}

func (c *logCollector) OnEmit(_ context.Context, record *sdklog.Record) error {
	c.records = append(c.records, record.Clone())
	return nil
}

func (*logCollector) Enabled(context.Context, sdklog.EnabledParameters) bool {
	return true
}

func (*logCollector) Shutdown(context.Context) error {
	return nil
}

func (*logCollector) ForceFlush(context.Context) error {
	return nil
}

func TestWriterPreservesBodyAndAttributes(t *testing.T) {
	collector := &logCollector{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(collector))
	ctx := otel.WithLoggerProvider(context.Background(), provider)
	writer := otel.NewWriter(ctx, "compatibility", attribute.String("source", "dagger"))

	if _, err := writer.Write([]byte("stdout")); err != nil {
		t.Fatal(err)
	}
	if err := provider.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	if len(collector.records) != 1 {
		t.Fatalf("got %d records, want 1", len(collector.records))
	}
	record := collector.records[0]
	if record.Body().AsString() != "stdout" {
		t.Fatalf("got body %q, want stdout", record.Body().AsString())
	}
	var attributes []attribute.KeyValue
	record.WalkAttributes(func(kv attribute.KeyValue) bool {
		attributes = append(attributes, kv)
		return true
	})
	if len(attributes) != 1 || attributes[0] != attribute.String("source", "dagger") {
		t.Fatalf("unexpected attributes: %v", attributes)
	}
	encoded := otel.LogsToPB(collector.records)[0].ScopeLogs[0].LogRecords[0]
	want := &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "stdout"}}
	if !proto.Equal(encoded.Body, want) || len(encoded.Attributes) != 1 || encoded.Attributes[0].Key != "source" {
		t.Fatalf("writer record changed during protobuf conversion: %v", encoded)
	}
}
