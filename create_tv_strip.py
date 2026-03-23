import requests

base_url = "http://127.0.0.1:29011"
entities_url = f"{base_url}/entities"

group_name = "BasementTVStrip"
lights = [
    "basement-14-tv-corner-left",
    "basement-02-tv-center-left",
    "basement-04-tv-center-center",
    "basement-17-tv-center-right",
    "basement-05-tv-right-corner"
]

def main():
    resp = requests.get(entities_url)
    entities = resp.json()

    # 1. Update entities with the correct labels and meta
    for entity in entities:
        data = entity["data"]
        name = data.get("name")
        if name in lights:
            # Update labels
            labels = data.get("labels", {})
            pa_labels = set(labels.get("PluginAutomation", []))
            pa_labels.add(group_name)
            labels["PluginAutomation"] = list(pa_labels)
            data["labels"] = labels
            
            # Update meta
            meta = data.get("meta", {})
            position = lights.index(name) + 1
            meta[f"PluginAutomation:{group_name}"] = {
                "position": position,
                "entity": "light_strip"
            }
            data["meta"] = meta
            
            plugin = data["plugin"]
            device = data["deviceID"]
            ent_id = data["id"]
            
            put_url = f"{entities_url}/{plugin}/{device}/{ent_id}"
            put_resp = requests.put(put_url, json=data)
            if put_resp.status_code >= 400:
                print(f"Failed to update {name}: {put_resp.text}")
            else:
                print(f"Updated {name} to have group {group_name} at position {position}")

    # 2. Create group entity in plugin-automation for BasementTVStrip
    group_id = group_name.lower()
    group_data = {
        "id": group_id,
        "plugin": "plugin-automation",
        "deviceID": "group",
        "type": "light_strip",
        "name": group_name,
        "labels": {
            "group_type": ["light_strip"]
        },
        "commands": [
            "light_turn_on",
            "light_turn_off",
            "light_set_brightness",
            "light_set_rgb",
            "light_set_color_temp",
            "lightstrip_set_segments"
        ],
        "state": {
            "power": False,
            "brightness": 0
        },
        "target": {
            "pattern": "",
            "where": [
                {
                    "field": "labels.PluginAutomation",
                    "op": "eq",
                    "value": group_name
                }
            ]
        }
    }
    
    put_url = f"{entities_url}/plugin-automation/group/{group_id}"
    put_resp = requests.put(put_url, json=group_data)
    if put_resp.status_code >= 400:
        print(f"Failed to create group {group_name}: {put_resp.text}")
    else:
        print(f"Created group {group_name}")

if __name__ == "__main__":
    main()
