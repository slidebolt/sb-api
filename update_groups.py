import requests
from script_env import get_base_url

base_url = get_base_url()
entities_url = f"{base_url}/entities"

groups_mapping = {
    "BasementBar": [
        "basement-16-bar-01", "basement-21-bar-02", "basement-20-bar-03", "basement-24-bar-04", "basement-15-bar-05"
    ],
    "BasementOffice": [
        "basement-26-office-01", "basement-27-office-02", "basement-28-office-03"
    ],
    "BasementEdison": [
        "basement-03-edison-01", "basement-08-edison-02", "basement-09-edison-01", "basement-10-edison-03", 
        "basement-12-edison-02", "basement-13-edison-03", "basement-11-edison-04"
    ],
    "BasementTV": [
        "basement-02-tv-center-left", "basement-04-tv-center-center", "basement-05-tv-right-corner",
        "basement-14-tv-corner-left", "basement-17-tv-center-right"
    ],
    "BasementMovieRoom": [
        "basement-06-movieroom-back-01", "basement-18-movieroom-back-02", "basement-23-movieroom-center",
        "basement-02-tv-center-left", "basement-04-tv-center-center", "basement-05-tv-right-corner",
        "basement-14-tv-corner-left", "basement-17-tv-center-right"
    ],
    "BasementBathroom": [
        "basement-29-bathroom-01", "basement-30-bathroom-02"
    ],
    "BasementHallways": [
        "basement-01-hallway-office", "basement-07-basement-hallway-01", "basement-19-hallway-bar",
        "basement-22-hallway-bathroom", "basement-25-hallway-stairs"
    ],
    "MasterBedroom": [
        "mbr_01", "mbr_02", "mbr_03", "mbr_04"
    ],
    "MasterBathroom": [
        "mbr_bath_01", "mbr_bath_02", "mbr_bath_03", "mbr_bath_04"
    ],
    "UpstairsBathroom": [
        "up_bath_01", "up_bath_02", "up_bath_03"
    ],
    "AlexRoom": [
        "alex_room_01", "alex_room_02"
    ],
    "JDBby": [
        "jdbby_01", "jdbby_02"
    ],
    "LEDLevels": [
        "LED Level 1", "LED Level 2", "LED Level 3", "LED Level 4"
    ]
}

def main():
    resp = requests.get(entities_url)
    entities = resp.json()
    
    # Pre-calculate mapping from entity name to all its target groups
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
            
            # Add all new groups
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

    # 2. Create group entities in plugin-automation for each group
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
