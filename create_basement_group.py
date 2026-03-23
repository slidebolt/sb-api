import requests

base_url = "http://127.0.0.1:29011"
entities_url = f"{base_url}/entities"

group_name = "Basement"

def main():
    resp = requests.get(entities_url)
    entities = resp.json()

    # 1. Update all basement lights to have the new label
    for entity in entities:
        data = entity["data"]
        name = data.get("name", "")
        etype = data.get("type", "")
        
        # If it's a light and "basement" is in the name, add it to the global Basement group
        if "basement" in name.lower() and (etype == "light" or etype == "light_strip"):
            labels = data.get("labels") or {}
            pa_labels = set(labels.get("PluginAutomation", []))
            
            # Add the new global group tag
            pa_labels.add(group_name)
            
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
                print(f"Added {name} to {group_name} group")

    # 2. Create the master group entity in plugin-automation
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
