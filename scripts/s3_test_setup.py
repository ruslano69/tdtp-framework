"""
s3_test_setup.py — підготувати SeaweedFS для тесту --map S3 input:
  1. Створити bucket travel-agency (якщо не існує)
  2. Завантажити локальний .tdtp.xml як customers/test.tdtp.xml
  3. Вивести список об'єктів в bucket для підтвердження
"""
import sys
import boto3
from botocore.exceptions import ClientError

ENDPOINT  = "http://127.0.0.1:8333"
REGION    = "us-east-1"
ACCESS    = "tdtp_access"
SECRET    = "tdtp_secret"
BUCKET    = "travel-agency"
KEY       = "customers/test.tdtp.xml"
LOCAL_SRC = r"H:\Ruslan\Code\Go\TDTP\tdtp-main-clean\out\emp_1072.tdtp.xml"

import botocore.config

s3 = boto3.client(
    "s3",
    endpoint_url=ENDPOINT,
    region_name=REGION,
    aws_access_key_id=ACCESS,
    aws_secret_access_key=SECRET,
    config=botocore.config.Config(signature_version="s3v4"),
)

# 1. Create bucket
try:
    s3.create_bucket(Bucket=BUCKET)
    print(f"[+] Bucket '{BUCKET}' created")
except ClientError as e:
    code = e.response["Error"]["Code"]
    if code in ("BucketAlreadyOwnedByYou", "BucketAlreadyExists"):
        print(f"[=] Bucket '{BUCKET}' already exists")
    else:
        sys.exit(f"[-] create_bucket error: {e}")

# 2. Upload test file
with open(LOCAL_SRC, "rb") as f:
    s3.put_object(Bucket=BUCKET, Key=KEY, Body=f)
print(f"[+] Uploaded: s3://{BUCKET}/{KEY}")

# 3. List objects
resp = s3.list_objects_v2(Bucket=BUCKET, Prefix="customers/")
for obj in resp.get("Contents", []):
    print(f"    {obj['Key']}  ({obj['Size']} bytes)")
