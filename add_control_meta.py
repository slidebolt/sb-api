import requests

base_url = "http://127.0.0.1:29011"
entities_url = f"{base_url}/entities"
group_name = "Basement"
control_switch_key = "plugin-esphome.switch_main_basement.switch_main_basement_3558733165"

def main():
    resp = requests.get(entities_url)
    entities = resp.json()

    # Find all entities in the Basement group
    basement_entities = []
    for entity in entities:
        data = entity["data"]
        labels = data.get("labels", {})
        pa_labels = labels.get("PluginAutomation", [])
        if group_name in pa_labels:
            basement_entities.append(entity)

    # Sort them by name just to give consistent positions
    basement_entities.sort(key=lambda e: e["data"].get("name", ""))

    for idx, entity in enumerate(basement_entities):
        data = entity["data"]
        name = data.get("name")
        etype = data.get("type", "light")

        meta = data.get("meta", {})
        pa_meta = meta.get(f"PluginAutomation:{group_name}", {})
        
        pa_meta["position"] = idx + 1
        pa_meta["entity"] = etype
        pa_meta["control"] = control_switch_key
        
        meta[f"PluginAutomation:{group_name}"] = pa_meta
        data["meta"] = meta
        
        plugin = data["plugin"]
        device = data["deviceID"]
        ent_id = data["id"]
        
        put_url = f"{entities_url}/{plugin}/{device}/{ent_id}"
        put_resp = requests.put(put_url, json=data)
        if put_resp.status_code >= 400:
            print(f"Failed to update {name}: {put_resp.text}")
        else:
            print(f"Updated {name} with control switch in meta")

if __name__ == "__main__":
    main()
