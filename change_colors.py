import requests

base_url = "http://127.0.0.1:29011"
entities_url = f"{base_url}/entities"

def main():
    resp = requests.get(entities_url)
    entities = resp.json()

    for entity in entities:
        data = entity["data"]
        name = data.get("name", "")
        etype = data.get("type", "")
        
        if "basement" in name.lower() and (etype == "light" or etype == "light_strip"):
            state = data.get("state", {})
            # Set to blue or something
            state["rgb"] = [0, 0, 255]
            data["state"] = state
            
            plugin = data["plugin"]
            device = data["deviceID"]
            ent_id = data["id"]
            
            put_url = f"{entities_url}/{plugin}/{device}/{ent_id}"
            put_resp = requests.put(put_url, json=data)
            if put_resp.status_code >= 400:
                print(f"Failed to update {name}: {put_resp.text}")
            else:
                print(f"Changed color of {name} to [0, 0, 255]")

if __name__ == "__main__":
    main()
