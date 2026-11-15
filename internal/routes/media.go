package routes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/slidebolt/sb-api/internal/auth"
	domain "github.com/slidebolt/sb-domain"
	media "github.com/slidebolt/sb-media-sdk"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

type catalogInput struct {
	Provider  string `path:"provider"`
	ServerID  string `path:"server"`
	LibraryID string `path:"library"`
	ParentID  string `query:"parent"`
	Query     string `query:"q"`
	Kind      string `query:"kind"`
	Sort      string `query:"sort"`
	Cursor    string `query:"cursor"`
	Limit     int    `query:"limit" minimum:"1" maximum:"200" default:"50"`
}

type itemInput struct {
	Provider  string `path:"provider"`
	ServerID  string `path:"server"`
	LibraryID string `path:"library"`
	ItemID    string `path:"item"`
}

type artworkInput struct {
	Provider string `path:"provider"`
	ServerID string `path:"server"`
	ItemID   string `path:"item"`
	Kind     string `path:"kind" enum:"thumb,art"`
}

type guideInput struct {
	Provider string    `path:"provider"`
	ServerID string    `path:"server"`
	Start    time.Time `query:"start" required:"true"`
	End      time.Time `query:"end" required:"true"`
	Channel  string    `query:"channel"`
	Query    string    `query:"q"`
	Cursor   string    `query:"cursor"`
	Limit    int       `query:"limit" minimum:"1" maximum:"500" default:"100"`
}

type catalogOutput struct{ Body media.CatalogPage }
type itemOutput struct{ Body media.MediaItem }
type artworkOutput struct {
	Body struct {
		ContentType string `json:"contentType"`
		Data        string `json:"data"`
	}
}
type guideOutput struct{ Body media.GuidePage }

func providerAllowed(ctx context.Context, store storage.Storage, provider, server, library string) bool {
	entries, err := store.Search(provider + "." + server + ".*")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		data, ok := storedEntity(entry.Key, entry.Data)
		if !ok {
			continue
		}
		var entity domain.Entity
		if json.Unmarshal(data, &entity) != nil {
			continue
		}
		if library != "" {
			state, ok := entity.State.(domain.MediaLibrary)
			if !ok || state.LibraryID != library {
				continue
			}
		}
		if auth.AllowsGroup(ctx, entity.SecurityGroup) {
			return true
		}
	}
	return false
}

func RegisterMedia(api huma.API, msg messenger.Messenger, store storage.Storage) {
	client := media.NewClient(msg)
	huma.Register(api, huma.Operation{Method: "GET", Path: "/media/catalog/{provider}/{server}/{library}", Summary: "Browse or search a media library", Tags: []string{"media"}}, func(ctx context.Context, in *catalogInput) (*catalogOutput, error) {
		if !providerAllowed(ctx, store, in.Provider, in.ServerID, in.LibraryID) {
			return nil, huma.Error404NotFound("media library not found")
		}
		page, err := client.Browse(in.Provider, media.CatalogRequest{ServerID: in.ServerID, LibraryID: in.LibraryID, ParentID: in.ParentID, Query: in.Query, Kind: in.Kind, Sort: in.Sort, Cursor: in.Cursor, Limit: in.Limit})
		if err != nil {
			return nil, huma.Error502BadGateway("media provider unavailable", err)
		}
		if page.Error != "" {
			return nil, huma.Error502BadGateway(page.Error)
		}
		return &catalogOutput{Body: page}, nil
	})

	huma.Register(api, huma.Operation{Method: "GET", Path: "/media/catalog/{provider}/{server}/{library}/items/{item}", Summary: "Get a media item", Tags: []string{"media"}}, func(ctx context.Context, in *itemInput) (*itemOutput, error) {
		if !providerAllowed(ctx, store, in.Provider, in.ServerID, in.LibraryID) {
			return nil, huma.Error404NotFound("media library not found")
		}
		page, err := client.Browse(in.Provider, media.CatalogRequest{ServerID: in.ServerID, LibraryID: in.LibraryID, ItemID: in.ItemID, Limit: 1})
		if err != nil {
			return nil, huma.Error502BadGateway("media provider unavailable", err)
		}
		if page.Error != "" {
			return nil, huma.Error502BadGateway(page.Error)
		}
		if len(page.Items) == 0 {
			return nil, huma.Error404NotFound("media item not found")
		}
		return &itemOutput{Body: page.Items[0]}, nil
	})

	huma.Register(api, huma.Operation{Method: "GET", Path: "/media/artwork/{provider}/{server}/{item}/{kind}", Summary: "Get proxied media artwork", Tags: []string{"media"}}, func(ctx context.Context, in *artworkInput) (*artworkOutput, error) {
		if !providerAllowed(ctx, store, in.Provider, in.ServerID, "") {
			return nil, huma.Error404NotFound("media item not found")
		}
		art, err := client.Artwork(in.Provider, media.ArtworkRequest{ServerID: in.ServerID, ItemID: in.ItemID, Kind: in.Kind})
		if err != nil {
			return nil, huma.Error502BadGateway("media provider unavailable", err)
		}
		if art.Error != "" {
			return nil, huma.Error502BadGateway(art.Error)
		}
		out := &artworkOutput{}
		out.Body.ContentType = art.ContentType
		out.Body.Data = base64.StdEncoding.EncodeToString(art.Data)
		return out, nil
	})

	huma.Register(api, huma.Operation{Method: "GET", Path: "/tv/guide/{provider}/{server}", Summary: "Browse or search a TV guide", Tags: []string{"television"}}, func(ctx context.Context, in *guideInput) (*guideOutput, error) {
		if !providerAllowed(ctx, store, in.Provider, in.ServerID, "") {
			return nil, huma.Error404NotFound("television provider not found")
		}
		if !in.End.After(in.Start) {
			return nil, huma.Error422UnprocessableEntity("end must be after start")
		}
		page, err := client.Guide(in.Provider, media.GuideRequest{ServerID: in.ServerID, Start: in.Start, End: in.End, Channel: in.Channel, Query: in.Query, Cursor: in.Cursor, Limit: in.Limit})
		if err != nil {
			return nil, huma.Error502BadGateway("media provider unavailable", err)
		}
		if page.Error != "" {
			return nil, huma.Error502BadGateway(page.Error)
		}
		return &guideOutput{Body: page}, nil
	})
}
