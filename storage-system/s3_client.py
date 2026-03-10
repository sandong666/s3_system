#!/usr/bin/env python3
"""
S3 私有桶客户端
支持上传、下载、列出、删除对象
"""

import hashlib
import hmac
import datetime
import requests
import os
import sys
import xml.etree.ElementTree as ET
from urllib.parse import quote, urlparse


DEBUG = os.environ.get('S3_DEBUG', '').lower() in ('1', 'true', 'yes')


class S3Client:
    """S3 客户端类"""

    def __init__(self, access_key, secret_key, endpoint, region='us-east-1'):
        self.access_key = access_key
        self.secret_key = secret_key
        self.endpoint = endpoint.rstrip('/')
        self.region = region
        self.service = 's3'

        # 解析 endpoint，提取 host 和 base_path
        parsed = urlparse(self.endpoint)
        self.host = parsed.netloc          # 例如 localhost:8080
        self.base_path = parsed.path       # 例如 /s3
        self.scheme = parsed.scheme        # 例如 http

    # ──────────────────────────────────────────
    #  签名工具
    # ──────────────────────────────────────────

    def _hmac_sha256(self, key, msg):
        return hmac.new(key, msg.encode('utf-8'), hashlib.sha256).digest()

    def _get_signature_key(self, date_stamp):
        k_date    = self._hmac_sha256(('AWS4' + self.secret_key).encode('utf-8'), date_stamp)
        k_region  = self._hmac_sha256(k_date, self.region)
        k_service = self._hmac_sha256(k_region, self.service)
        k_signing = self._hmac_sha256(k_service, 'aws4_request')
        return k_signing

    def _build_headers(self, method, resource_path, query_string='',
                       extra_headers=None, payload=b''):
        """
        构建带 AWS SigV4 签名的请求头

        resource_path: 不含 base_path 的资源路径，如 /private/t2.txt
        """
        now = datetime.datetime.now(datetime.timezone.utc)
        amz_date   = now.strftime('%Y%m%dT%H%M%SZ')
        date_stamp = now.strftime('%Y%m%d')

        # ★ 关键：签名用的 canonical URI = base_path + resource_path
        # 例如 /s3/private/t2.txt
        canonical_uri = self.base_path + resource_path

        payload_hash = hashlib.sha256(payload).hexdigest()

        # ---- 只签这三个核心头 ----
        sign_headers = {
            'host':                 self.host,
            'x-amz-content-sha256': payload_hash,
            'x-amz-date':          amz_date,
        }

        sorted_keys = sorted(sign_headers.keys())
        canonical_headers = ''.join(f'{k}:{sign_headers[k]}\n' for k in sorted_keys)
        signed_headers_str = ';'.join(sorted_keys)

        # ---- 规范查询字符串（按 key 排序）----
        if query_string:
            qs_parts = sorted(query_string.split('&'))
            canonical_qs = '&'.join(qs_parts)
        else:
            canonical_qs = ''

        # ---- 规范请求 ----
        canonical_request = '\n'.join([
            method,
            canonical_uri,
            canonical_qs,
            canonical_headers,
            signed_headers_str,
            payload_hash,
        ])

        # ---- 待签名字符串 ----
        credential_scope = f'{date_stamp}/{self.region}/{self.service}/aws4_request'
        string_to_sign = '\n'.join([
            'AWS4-HMAC-SHA256',
            amz_date,
            credential_scope,
            hashlib.sha256(canonical_request.encode('utf-8')).hexdigest(),
        ])

        # ---- 计算签名 ----
        signing_key = self._get_signature_key(date_stamp)
        signature = hmac.new(
            signing_key, string_to_sign.encode('utf-8'), hashlib.sha256
        ).hexdigest()

        authorization = (
            f'AWS4-HMAC-SHA256 '
            f'Credential={self.access_key}/{credential_scope}, '
            f'SignedHeaders={signed_headers_str}, '
            f'Signature={signature}'
        )

        # ---- DEBUG 输出 ----
        if DEBUG:
            print('=' * 60)
            print('[DEBUG] Canonical Request:')
            print(canonical_request)
            print('-' * 60)
            print('[DEBUG] String to Sign:')
            print(string_to_sign)
            print('-' * 60)
            print(f'[DEBUG] Signature: {signature}')
            print(f'[DEBUG] Authorization: {authorization}')
            print('=' * 60)

        result = {
            'x-amz-date':          amz_date,
            'x-amz-content-sha256': payload_hash,
            'Authorization':        authorization,
        }
        # Content-Type 等额外头只发送，不参与签名
        if extra_headers:
            result.update(extra_headers)

        return result

    def _request(self, method, resource_path, query_string='',
                 extra_headers=None, payload=b''):
        """发送签名请求"""
        headers = self._build_headers(
            method, resource_path, query_string, extra_headers, payload
        )
        url = f'{self.endpoint}{resource_path}'
        if query_string:
            url += f'?{query_string}'

        if DEBUG:
            print(f'[DEBUG] {method} {url}')

        resp = requests.request(method, url, headers=headers, data=payload)
        return resp

    # ──────────────────────────────────────────
    #  业务方法
    # ──────────────────────────────────────────

    def list_buckets(self):
        resp = self._request('GET', '/')
        if resp.status_code != 200:
            print(f'[ERROR] list_buckets 失败: {resp.status_code}\n{resp.text}')
            return []
        root = ET.fromstring(resp.text)
        ns = ''
        if root.tag.startswith('{'):
            ns = root.tag.split('}')[0] + '}'
        buckets = []
        for b in root.iter(f'{ns}Bucket'):
            name = b.find(f'{ns}Name')
            if name is not None:
                buckets.append(name.text)
        print(f'共 {len(buckets)} 个桶: {buckets}')
        return buckets

    def list_objects(self, bucket, prefix='', max_keys=1000):
        qs = f'list-type=2&max-keys={max_keys}&prefix={quote(prefix, safe="")}'
        resp = self._request('GET', f'/{bucket}', query_string=qs)
        if resp.status_code != 200:
            print(f'[ERROR] list_objects 失败: {resp.status_code}\n{resp.text}')
            return []
        root = ET.fromstring(resp.text)
        ns = ''
        if root.tag.startswith('{'):
            ns = root.tag.split('}')[0] + '}'
        objects = []
        for item in root.iter(f'{ns}Contents'):
            key  = item.find(f'{ns}Key')
            size = item.find(f'{ns}Size')
            lm   = item.find(f'{ns}LastModified')
            objects.append({
                'Key':          key.text  if key  is not None else '',
                'Size':         size.text if size is not None else '0',
                'LastModified': lm.text   if lm   is not None else '',
            })
        print(f'桶 [{bucket}] 共 {len(objects)} 个对象:')
        for o in objects:
            print(f'  {o["Key"]:40s}  {o["Size"]:>10s} B  {o["LastModified"]}')
        return objects

    def upload_file(self, bucket, key, file_path):
        if not os.path.isfile(file_path):
            print(f'[ERROR] 文件不存在: {file_path}')
            return False
        with open(file_path, 'rb') as f:
            payload = f.read()
        uri = f'/{bucket}/{quote(key, safe="/")}'
        extra = {'Content-Type': 'application/octet-stream'}
        resp = self._request('PUT', uri, extra_headers=extra, payload=payload)
        if resp.status_code in (200, 201):
            print(f'[OK] 上传成功: {file_path} -> {bucket}/{key}')
            return True
        else:
            print(f'[ERROR] 上传失败: {resp.status_code}\n{resp.text}')
            return False

    def download_file(self, bucket, key, output_path=None):
        if output_path is None:
            output_path = os.path.basename(key)
        uri = f'/{bucket}/{quote(key, safe="/")}'
        resp = self._request('GET', uri)
        if resp.status_code == 200:
            os.makedirs(os.path.dirname(output_path) or '.', exist_ok=True)
            with open(output_path, 'wb') as f:
                f.write(resp.content)
            print(f'[OK] 下载成功: {bucket}/{key} -> {output_path} ({len(resp.content)} bytes)')
            return True
        else:
            print(f'[ERROR] 下载失败: {resp.status_code}\n{resp.text}')
            return False

    def delete_object(self, bucket, key):
        uri = f'/{bucket}/{quote(key, safe="/")}'
        resp = self._request('DELETE', uri)
        if resp.status_code in (200, 204):
            print(f'[OK] 删除成功: {bucket}/{key}')
            return True
        else:
            print(f'[ERROR] 删除失败: {resp.status_code}\n{resp.text}')
            return False

    def head_object(self, bucket, key):
        uri = f'/{bucket}/{quote(key, safe="/")}'
        resp = self._request('HEAD', uri)
        if resp.status_code == 200:
            info = dict(resp.headers)
            print(f'[OK] {bucket}/{key} 元信息:')
            for k, v in info.items():
                print(f'  {k}: {v}')
            return info
        else:
            print(f'[ERROR] HEAD 失败: {resp.status_code}')
            return None


# ──────────────────────────────────────────────
#  命令行入口
# ──────────────────────────────────────────────

def print_usage():
    print("""
用法:
  set S3_ACCESS_KEY=AK-xxx
  set S3_SECRET_KEY=SK-xxx
  set S3_ENDPOINT=http://localhost:8080/s3
  set S3_DEBUG=1                          (可选, 打印签名调试信息)

命令:
  python s3_client.py list-buckets
  python s3_client.py ls       <bucket> [prefix]
  python s3_client.py upload   <bucket> <key> <file>
  python s3_client.py download <bucket> <key> [output]
  python s3_client.py delete   <bucket> <key>
  python s3_client.py head     <bucket> <key>
""".strip())


def main():
    if len(sys.argv) < 2:
        print_usage()
        sys.exit(1)

    ak       = os.environ.get('S3_ACCESS_KEY', '')
    sk       = os.environ.get('S3_SECRET_KEY', '')
    endpoint = os.environ.get('S3_ENDPOINT', '')
    region   = os.environ.get('S3_REGION', 'us-east-1')

    if not ak or not sk or not endpoint:
        print('[ERROR] 请设置环境变量: S3_ACCESS_KEY, S3_SECRET_KEY, S3_ENDPOINT')
        sys.exit(1)

    client = S3Client(ak, sk, endpoint, region)
    cmd = sys.argv[1]

    if cmd == 'list-buckets':
        client.list_buckets()
    elif cmd == 'ls':
        if len(sys.argv) < 3:
            print('[ERROR] 用法: ls <bucket> [prefix]'); sys.exit(1)
        client.list_objects(sys.argv[2], sys.argv[3] if len(sys.argv) > 3 else '')
    elif cmd == 'upload':
        if len(sys.argv) < 5:
            print('[ERROR] 用法: upload <bucket> <key> <file>'); sys.exit(1)
        client.upload_file(sys.argv[2], sys.argv[3], sys.argv[4])
    elif cmd == 'download':
        if len(sys.argv) < 4:
            print('[ERROR] 用法: download <bucket> <key> [output]'); sys.exit(1)
        client.download_file(sys.argv[2], sys.argv[3],
                             sys.argv[4] if len(sys.argv) > 4 else None)
    elif cmd == 'delete':
        if len(sys.argv) < 4:
            print('[ERROR] 用法: delete <bucket> <key>'); sys.exit(1)
        client.delete_object(sys.argv[2], sys.argv[3])
    elif cmd == 'head':
        if len(sys.argv) < 4:
            print('[ERROR] 用法: head <bucket> <key>'); sys.exit(1)
        client.head_object(sys.argv[2], sys.argv[3])
    else:
        print(f'[ERROR] 未知命令: {cmd}')
        print_usage()
        sys.exit(1)


if __name__ == '__main__':
    main()