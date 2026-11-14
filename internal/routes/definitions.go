package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/invopop/jsonschema"
	contract "github.com/slidebolt/sb-contract"
	domain "github.com/slidebolt/sb-domain"
	storage "github.com/slidebolt/sb-storage-sdk"
)

type SerializedDefinition struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	LiveCount   int             `json:"liveCount"`
	Producers   []string        `json:"producers,omitempty"`
}

type DomainCatalog struct {
	DeviceSchema json.RawMessage        `json:"deviceSchema"`
	EntityTypes  []SerializedDefinition `json:"entityTypes"`
	Commands     []SerializedDefinition `json:"commands"`
}

type PluginCatalog struct {
	ID                  string                      `json:"id"`
	State               string                      `json:"state"`
	Definitions         *contract.PluginDefinitions `json:"definitions,omitempty"`
	ObservedEntityTypes []string                    `json:"observedEntityTypes,omitempty"`
	DefinitionError     string                      `json:"definitionError,omitempty"`
}

type CoverageSection struct {
	Total      int      `json:"total"`
	Resolved   int      `json:"resolved"`
	Unresolved []string `json:"unresolved"`
}

type DefinitionsBody struct {
	SchemaVersion int             `json:"schemaVersion"`
	Domain        DomainCatalog   `json:"domain"`
	Plugins       []PluginCatalog `json:"plugins"`
	Coverage      struct {
		Devices  CoverageSection `json:"devices"`
		Entities CoverageSection `json:"entities"`
		Commands CoverageSection `json:"commands"`
	} `json:"coverage"`
}

type DefinitionsOutput struct{ Body DefinitionsBody }

func schemaFor(value any) json.RawMessage {
	schema := jsonschema.Reflect(value)
	data, _ := json.Marshal(schema)
	return data
}

func RegisterDefinitions(api huma.API, store storage.Storage, managerURL string) {
	client := &http.Client{Timeout: 2 * time.Second}
	huma.Register(api, huma.Operation{Method: "GET", Path: "/definitions", Summary: "List serialized domain and plugin definitions", Description: "Returns code-owned schemas and descriptions plus live coverage. The response contains no plugin-specific presentation logic.", Tags: []string{"definitions"}}, func(ctx context.Context, _ *struct{}) (*DefinitionsOutput, error) {
		body := DefinitionsBody{SchemaVersion: 1}
		body.Domain.DeviceSchema = schemaFor(domain.Device{})

		entityDefs := domain.EntityDefinitions()
		entityIndex := make(map[string]int, len(entityDefs))
		for _, def := range entityDefs {
			entityIndex[def.Type] = len(body.Domain.EntityTypes)
			body.Domain.EntityTypes = append(body.Domain.EntityTypes, SerializedDefinition{Type: def.Type, Name: def.Name, Description: def.Description, Schema: schemaFor(reflect.New(def.GoType).Elem().Interface())})
		}
		commandDefs := domain.CommandDefinitions()
		commandKnown := make(map[string]struct{}, len(commandDefs))
		for _, def := range commandDefs {
			commandKnown[def.Type] = struct{}{}
			body.Domain.Commands = append(body.Domain.Commands, SerializedDefinition{Type: def.Type, Name: def.Name, Description: def.Description, Schema: schemaFor(reflect.New(def.GoType).Elem().Interface())})
		}

		statuses, err := fetchDefinitionStatuses(ctx, client, managerURL)
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("manager definitions unavailable", err)
		}
		plugins := make(map[string]*PluginCatalog)
		pluginCommands := make(map[string]struct{})
		for _, status := range statuses {
			if status.Kind != contract.KindPlugin {
				continue
			}
			catalog := &PluginCatalog{ID: status.ID, State: status.State, Definitions: status.Definitions}
			if status.Definitions == nil {
				catalog.DefinitionError = "plugin did not serialize a definition manifest"
			}
			plugins[status.ID] = catalog
			if status.Definitions != nil {
				for _, command := range status.Definitions.Commands {
					pluginCommands[command.Action] = struct{}{}
				}
			}
		}

		devices, _ := store.Search("*.*")
		for _, entry := range devices {
			data, ok := storedDevice(entry.Key, entry.Data)
			if !ok {
				continue
			}
			body.Coverage.Devices.Total++
			var device domain.Device
			_ = json.Unmarshal(data, &device)
			// Every valid device is an instance of the shared domain Device
			// contract. Class is optional specialization data; when supplied it
			// must be declared by the producing plugin.
			if device.Class == "" || hasDeviceClass(plugins[device.Plugin], device.Class) {
				body.Coverage.Devices.Resolved++
			} else {
				body.Coverage.Devices.Unresolved = append(body.Coverage.Devices.Unresolved, entry.Key)
			}
		}

		entities, _ := store.Search("*.*.*")
		producerSets := map[string]map[string]struct{}{}
		observed := map[string]map[string]struct{}{}
		usedCommands := map[string]struct{}{}
		for _, entry := range entities {
			data, ok := storedEntity(entry.Key, entry.Data)
			if !ok {
				continue
			}
			body.Coverage.Entities.Total++
			var entity domain.Entity
			_ = json.Unmarshal(data, &entity)
			if observed[entity.Plugin] == nil {
				observed[entity.Plugin] = map[string]struct{}{}
			}
			observed[entity.Plugin][entity.Type] = struct{}{}
			if idx, ok := entityIndex[entity.Type]; ok {
				body.Coverage.Entities.Resolved++
				body.Domain.EntityTypes[idx].LiveCount++
				if producerSets[entity.Type] == nil {
					producerSets[entity.Type] = map[string]struct{}{}
				}
				producerSets[entity.Type][entity.Plugin] = struct{}{}
			} else if pluginDefines(plugins[entity.Plugin], entity.Type) {
				body.Coverage.Entities.Resolved++
			} else {
				body.Coverage.Entities.Unresolved = append(body.Coverage.Entities.Unresolved, entry.Key)
			}
			for _, action := range entity.Commands {
				usedCommands[action] = struct{}{}
			}
		}
		for i := range body.Domain.EntityTypes {
			body.Domain.EntityTypes[i].Producers = sortedSet(producerSets[body.Domain.EntityTypes[i].Type])
		}
		body.Coverage.Commands.Total = len(usedCommands)
		for action := range usedCommands {
			_, domainOK := commandKnown[action]
			_, pluginOK := pluginCommands[action]
			if domainOK || pluginOK {
				body.Coverage.Commands.Resolved++
			} else {
				body.Coverage.Commands.Unresolved = append(body.Coverage.Commands.Unresolved, action)
			}
		}

		for id, catalog := range plugins {
			catalog.ObservedEntityTypes = sortedSet(observed[id])
			body.Plugins = append(body.Plugins, *catalog)
		}
		sort.Slice(body.Plugins, func(i, j int) bool { return body.Plugins[i].ID < body.Plugins[j].ID })
		sort.Strings(body.Coverage.Devices.Unresolved)
		sort.Strings(body.Coverage.Entities.Unresolved)
		sort.Strings(body.Coverage.Commands.Unresolved)
		return &DefinitionsOutput{Body: body}, nil
	})
}

type definitionStatus struct {
	ID, Kind, State string
	Definitions     *contract.PluginDefinitions
}

func fetchDefinitionStatuses(ctx context.Context, client *http.Client, managerURL string) ([]definitionStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(managerURL, "/")+"/status/components", nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out []definitionStatus
	err = json.NewDecoder(res.Body).Decode(&out)
	return out, err
}
func hasDeviceClass(plugin *PluginCatalog, class string) bool {
	if plugin == nil || plugin.Definitions == nil {
		return false
	}
	for _, d := range plugin.Definitions.DeviceClasses {
		if d.Class == class {
			return true
		}
	}
	return false
}
func pluginDefines(plugin *PluginCatalog, typ string) bool {
	if plugin == nil || plugin.Definitions == nil {
		return false
	}
	for _, d := range plugin.Definitions.Extensions {
		if d.Type == typ {
			return true
		}
	}
	return false
}
func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
