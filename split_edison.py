import requests

base_url = "http://127.0.0.1:29011"
entities_url = f"{base_url}/entities"

# Define the new groupings
groups_mapping = {
    "BasementMovieRoomEdison": [
        "basement-03-edison-01",
        "basement-08-edison-02",
        "basement-13-edison-03"
    ],
    "BasementBarEdison": [
        "basement-09-edison-01",
        "basement-12-edison-02",
        "basement-10-edison-03",
        "basement-11-edison-04"
    ]
}

def main():
    resp = requests.get(entities_url)
    entities = resp.json()
    
    # Pre-calculate mapping from entity name to its new target groups
    entity_to_groups = {}
    for group_name, members in groups_mapping.items():
        for member in members:
            if member not in entity_to_groups:
                entity_to_groups[member] = set()
            entity_to_groups[member].add(group_name)

    # 1. Update entities with the correct labels
    for entity in entities:
        data = entity["data"]
        name = data.get("name")
        if name in entity_to_groups:
            labels = data.get("labels") or {}
            pa_labels = set(labels.get("PluginAutomation", []))
            
            # Add new subgroup tags
            pa_labels.update(entity_to_groups[name])
            
            labels["PluginAutomation"] = list(pa_labels)
            data["labels"] = labels
            
            plugin = data["plugin"]
            device = data["deviceID"]
            ent_id = data["id"]
            
            put_url = f"{entities_url}/{plugin}/{device}/{ent_id}"
            put_resp = requests.put(put_url, json=data)
            if put_resp.status_code >= 400:
                print(f"Failed to update {name}: {put_resp.text}")
            else:
                print(f"Updated {name} to have groups: {labels['PluginAutomation']}")

    # 2. Create group entities in plugin-automation for each new group
    for group_name in groups_mapping.keys():
        group_id = group_name.lower()
        group_data = {
            "id": group_id,
            "plugin": "plugin-automation",
            "deviceID": "group",
            "type": "light",
            "name": group_name,
            "labels": {
                "group_type": ["light"]
            },
            "commands": [
                "light_turn_on",
                "light_turn_off",
                "light_set_brightness",
                "light_set_rgb",
                "light_set_color_temp"
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
