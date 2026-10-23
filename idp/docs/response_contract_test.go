package docs

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAuthenticationResponseContractsInEveryPublishedDocument(t *testing.T) {
	jsonBytes, err := os.ReadFile("swagger.json")
	if err != nil {
		t.Fatal(err)
	}
	yamlBytes, err := os.ReadFile("swagger.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]any
	if err := json.Unmarshal(jsonBytes, &expected); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"json": jsonBytes, "yaml": yamlBytes, "served": []byte(SwaggerInfo.ReadDoc()),
	} {
		t.Run(name, func(t *testing.T) {
			var document map[string]any
			if name == "yaml" {
				err = yaml.Unmarshal(data, &document)
			} else {
				err = json.Unmarshal(data, &document)
			}
			if err != nil {
				t.Fatal(err)
			}
			// All published forms must agree, not only the repaired response fields.
			for _, key := range []string{"paths", "definitions"} {
				if !reflect.DeepEqual(document[key], expected[key]) {
					t.Errorf("%s differs from the checked-in JSON document", key)
				}
			}
			paths := objectAt(t, document, "paths")
			for _, tc := range []struct{ path, ref string }{
				{"/auth/register", "#/definitions/dto.UserResponse"},
				{"/auth/request-password-reset", "#/definitions/dto.SuccessResponse"},
				{"/auth/confirm-password-reset", "#/definitions/dto.SuccessResponse"},
			} {
				schema := objectAt(t, paths, tc.path, "post", "responses", "200", "schema")
				if schema["$ref"] != tc.ref {
					t.Errorf("%s response schema = %v; want %s", tc.path, schema, tc.ref)
				}
			}
			definitions := objectAt(t, document, "definitions")
			properties := objectAt(t, definitions, "dto.SuccessResponse", "properties")
			for key, kind := range map[string]string{"message": "string", "statusCode": "integer"} {
				if objectAt(t, properties, key)["type"] != kind {
					t.Errorf("success response is missing %s of type %s", key, kind)
				}
			}
			login := objectAt(t, paths, "/auth/login", "post", "responses")
			objectAt(t, login, "401")
			if objectAt(t, login, "400")["description"] != "Bad Request" {
				t.Error("login malformed input must not be documented as unauthorized")
			}
			logout := objectAt(t, paths, "/auth/logout", "post")
			responses := objectAt(t, logout, "responses")
			objectAt(t, responses, "401")
			objectAt(t, responses, "403")
			if _, present := responses["400"]; present {
				t.Error("logout documents a status it does not return")
			}
			parameters, ok := logout["parameters"].([]any)
			if !ok || len(parameters) != 1 {
				t.Fatal("logout must document its required Origin header")
			}
			origin, ok := parameters[0].(map[string]any)
			if !ok || origin["name"] != "Origin" || origin["in"] != "header" || origin["required"] != true {
				t.Error("logout Origin header contract is incorrect")
			}
		})
	}
}

func objectAt(t *testing.T, value map[string]any, keys ...string) map[string]any {
	t.Helper()
	for _, key := range keys {
		next, ok := value[key].(map[string]any)
		if !ok {
			t.Fatalf("missing document object %s", key)
		}
		value = next
	}
	return value
}
