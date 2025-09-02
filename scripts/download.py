import os
import time
import requests
from huggingface_hub import snapshot_download, HfApi
from huggingface_hub.utils import RepositoryNotFoundError
from requests.exceptions import RequestException

MODEL_NAME = os.getenv("MODEL_NAME", "")
MODEL_PATH = os.getenv("MODEL_PATH", "")
HF_TOKEN = os.getenv("HF_TOKEN", None)  # Read Hugging Face Token
MAX_RETRIES = 3
RETRY_DELAY = 10  # Seconds


print(f"{MODEL_NAME} {MODEL_PATH}")
def login_huggingface():
    """Attempt to authenticate with Hugging Face."""
    if HF_TOKEN:
        try:
            api = HfApi()
            api.whoami(token=HF_TOKEN)  # Validate the token
            print("Authentication successful: Hugging Face access granted.")
            return True
        except Exception as e:
            print(f"Authentication failed: {e}")
            return False
    return True  # Continue for public models

def check_model_exists():
    """Check if the given Hugging Face repository exists before downloading."""
    api = HfApi()
    try:
        api.model_info(repo_id=MODEL_NAME, token=HF_TOKEN)  # Check repo existence
        print(f"Model repository '{MODEL_NAME}' exists. Proceeding with download.")
        return True
    except RepositoryNotFoundError:
        print(f"Error: Model repository '{MODEL_NAME}' not found. Exiting!")
        return False
    except Exception as e:
        print(f"Unexpected error while checking model existence: {e}")
        return False

def download_model():
    """Download the specified model with retry logic."""
    if not login_huggingface():
        return 1  # Exit on authentication failure

    if not check_model_exists():
        return 1

    for attempt in range(MAX_RETRIES):
        try:
            print(
                f"Starting model download: {MODEL_NAME} to {MODEL_PATH} (Attempt {attempt + 1}/{MAX_RETRIES})"
            )
            ret=snapshot_download(
                repo_id=MODEL_NAME,
                local_dir=MODEL_PATH,
                token=HF_TOKEN,  # Use Token if provided
            )
            print(f"Model download completed successfully!{ret}")
            return 0  # Exit successfully
        except requests.exceptions.HTTPError as e:
            if e.response.status_code in [401, 403, 404]:
                print(
                    f"Critical error: HTTP {e.response.status_code}. Download cannot proceed. Terminating!"
                )
                return 1  # Exit on fatal errors
        except RequestException as e:
            print(f"Network error: {e}. Retrying in {RETRY_DELAY} seconds...")
            time.sleep(RETRY_DELAY)

    print("Maximum retry attempts reached. Download failed.")
    return 1  # Exit after exhausting retries


if __name__ == "__main__":
    exit(download_model())
