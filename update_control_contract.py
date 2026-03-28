import requests
from script_env import get_base_url

base_url = get_base_url()
entities_url = f"{base_url}/entities"

group_name = "Basement"
switch_device_id = "switch_main_basement"
switch_entity_id = "switch_main_basement_3558733165"
switch_plugin = "plugin-esphome"

def main():
    resp = requests.get(entities_url)
    entities = resp.json()

    # 1. Clean up the old metadata approach from the lights
    for entity in entities:
        data = entity["data"]
        labels = data.get("labels", {})
        pa_labels = labels.get("PluginAutomation", [])
        
        if group_name in pa_labels and data.get("id") != switch_entity_id:
            meta = data.get("meta", {})
            pa_meta = meta.get(f"PluginAutomation:{group_name}", {})
            
            # Remove the control key we incorrectly added earlier
            if "control" in pa_meta:
                del pa_meta["control"]
                meta[f"PluginAutomation:{group_name}"] = pa_meta
                data["meta"] = meta
                
                plugin = data["plugin"]
                device = data["deviceID"]
                ent_id = data["id"]
                
                put_url = f"{entities_url}/{plugin}/{device}/{ent_id}"
                requests.put(put_url, json=data)

    # 2. Update the Switch entity using the new contract
    switch_url = f"{entities_url}/{switch_plugin}/{switch_device_id}/{switch_entity_id}"
    switch_resp = requests.get(switch_url)
    
    if switch_resp.status_code == 200:
        switch_data = switch_resp.json()
        
        # Add group to labels
        labels = switch_data.get("labels", {})
        pa_labels = set(labels.get("PluginAutomation", []))
        pa_labels.add(group_name)
        labels["PluginAutomation"] = list(pa_labels)
        switch_data["labels"] = labels
        
        # Apply the new meta contract
        meta = switch_data.get("meta", {})
        meta[f"PluginAutomation:{group_name}"] = {
            "position": 0,
            "entity": "switch",
            "control": True
        }
        switch_data["meta"] = meta
        
        put_resp = requests.put(switch_url, json=switch_data)
        if put_resp.status_code >= 400:
            print(f"Failed to update switch: {put_resp.text}")
        else:
            print("Successfully updated the switch with the new control contract.")
    else:
        print("Could not fetch the switch entity.")

if __name__ == "__main__":
    main()
