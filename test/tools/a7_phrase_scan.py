#!/usr/bin/env python3
# 使い方(リポジトリの根から): python3 test/tools/a7_phrase_scan.py <record.jsonl>... / --selftest
# 記録の中で BIP39 の語が続く最長の長さを数える(フレーズの先頭レコードの漏れの代わりの確かめ)。英字の語だけで数え、記号は区切りとして飛ばす。
import json,re,sys
S=set(json.load(open('app/src/data/bip39-english.json')))
def longest(t):
    t=re.sub(r'\\[nrt"\\/]',' ',t)
    run=best=0
    for x in re.findall(r'[A-Za-z]+',t):
        if x in S: run+=1; best=max(best,run)
        else: run=0
    return best
if __name__=='__main__':
    if sys.argv[1]=='--selftest':
        W=json.load(open('app/src/data/bip39-english.json'))
        print([longest(json.dumps({"words":W[:24]})), longest(' '.join(W[100:124])), longest(json.dumps({"text":"\n".join(W[5:29])})), longest(json.dumps({"body":{"messages":[{"content":json.dumps({"schema":"sitate.secret-string/v1","words":W[200:224]})}]}}))])
    else:
        for f in sys.argv[1:]:
            t=open(f).read(); print(f, 'schema', t.count('sitate.secret-string'), 'longest_bip39_run', longest(t))
