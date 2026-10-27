"""Validate owned hosted SDK outputs before using them as a runtime context."""
from pathlib import Path
import argparse
import hashlib
import json
import os
import re
import sys


def pins_and_hashes(raw):
    result={}
    current=None
    for line in raw.decode('utf-8').splitlines():
        match=re.fullmatch(r'([A-Za-z0-9_.-]+)==([^\s;\\]+)\s*\\?',line)
        if match:
            current=match.groups()
            if current in result:raise ValueError('Duplicate selected package')
            result[current]=set()
        else:
            digest=re.fullmatch(r'\s+--hash=sha256:([0-9a-f]{64})\s*\\?',line)
            if digest:
                if current is None:raise ValueError('Hash precedes package')
                result[current].add(digest.group(1))
            elif line.strip() and not line.lstrip().startswith('#') and line!='--index-url https://pypi.org/simple':
                raise ValueError('Unexpected lock source or syntax')
    if not result or any(not hashes for hashes in result.values()):raise ValueError('Missing lock hashes')
    return result


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output',type=Path,required=True)
    args=parser.parse_args()
    if (os.environ.get('GITHUB_ACTIONS')!='true' or sys.platform!='linux'
            or not os.environ.get('GITHUB_RUN_ID','').isdigit()
            or not os.environ.get('GITHUB_RUN_ATTEMPT','').isdigit()):
        raise RuntimeError('Runtime context preparation requires the identified hosted SDK job')
    output=args.output.resolve(strict=True)
    temporary=Path(os.environ['RUNNER_TEMP']).resolve(strict=True)
    if not output.is_relative_to(temporary) or output==temporary:
        raise ValueError('Expected an owned hosted temporary output directory')
    compat=Path(__file__).resolve().parent
    service=compat.parent
    sha=lambda raw:hashlib.sha256(raw).hexdigest()
    receipt=json.loads((output/'receipt.json').read_text(encoding='utf-8'))
    if (receipt['source_revision']!=os.environ['GITHUB_SHA'] or receipt['returncode']!=0
            or not receipt['public_sdk_import_qualified'] or not receipt['unchanged_upstream_cases_qualified']):
        raise ValueError('Current checkout SDK and unchanged vendor cases must qualify first')
    if (output/'inputs/requirements.in').read_bytes()!=(service/'requirements.in').read_bytes():
        raise ValueError('Canonical inputs differ from the qualified SDK inputs')
    actual=(output/'outputs/requirements.lock').read_bytes()
    canonical=(service/'requirements.lock').read_bytes()
    selected=pins_and_hashes(canonical)
    if pins_and_hashes(actual)!=selected or len(selected)!=83:
        raise ValueError('Regenerated runtime graph differs from reviewed canonical pins/hashes')
    if canonical!=(service/'requirements.txt').read_bytes():raise ValueError('Legacy lock mirror differs')
    sdk=json.loads((output/'qualification/qualification.json').read_text(encoding='utf-8'))
    cases=json.loads((output/'upstream-case-results/qualification.json').read_text(encoding='utf-8'))
    if not sdk['qualified'] or not cases['qualified']:
        raise ValueError('Required SDK behavior qualification is missing')
    counts={row['suite']:row['passed'] for row in cases['suites']}
    if counts!={'fastuuid':48,'datasets':11,'botocore':21}:
        raise ValueError('Unexpected unchanged vendor case coverage')
    for name in ['upstream-case-specs.json','run_upstream_cases.py','verify_sdk_import.py']:
        if (output/'inputs'/name).read_bytes()!=(compat/name).read_bytes():
            raise ValueError('Executed qualification input differs from checkout')
    build=json.loads((output/'fastuuid-build.json').read_text(encoding='utf-8'))
    if build['source_port_spec_sha256']!=sha((compat/'fastuuid-port-spec.json').read_bytes()):
        raise ValueError('Native build specification differs')
    wheels={}
    for path in (output/'inputs').glob('*.whl'):
        digest=sha(path.read_bytes())
        if not any(digest in hashes for hashes in selected.values()):
            raise ValueError('Prepared wheel is absent from canonical graph hashes')
        wheels[path.name]=digest
    if len(wheels)!=4:raise ValueError('Expected exactly four reviewed source-port wheels')
    (output/'inputs/upstream-case-qualification.json').write_bytes((output/'upstream-case-results/qualification.json').read_bytes())
    proof={'source_revision':receipt['source_revision'],'canonical_lock_sha256':sha(canonical),
           'generated_lock_sha256':sha(actual),'canonical_pins_and_hashes_match':True,
           'wheels':wheels,'unchanged_vendor_case_counts':counts,
           'SDK_and_vendor_cases_qualified':True,'runtime_image_qualified':False,
           'operator_source_credentials_or_database_mounted':False}
    (output/'runtime-context.json').write_text(json.dumps(proof,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(proof))


if __name__=='__main__':main()
