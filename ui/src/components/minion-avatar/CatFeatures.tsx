import type { CatCoat } from './catCharacters';

export function CatPaw() {
  return <g className="cat-paw">
    <path className="cat-fur" d="M-7-5Q0-8 7-5L10 6Q15 21 6 25Q0 29-7 25Q-15 21-10 7Z" />
    <ellipse cx="0" cy="15" rx="5" ry="6" fill="#c58d8e" />
    <path d="M-6 21v3M0 23v3M6 21v3" stroke="var(--stripe)" strokeWidth="1.3" strokeLinecap="round" />
    <path className="tabby-stripe" d="M-7 0H7" />
  </g>;
}

export function CatExtremities() {
  return <>
    <g className="cat-tail">
      <path d="M140 206Q185 228 180 186Q174 162 186 151" fill="none" stroke="var(--fur)" strokeWidth="13" strokeLinecap="round" />
      <path className="tail-stripes" d="M179 181l-1-8m4-14 4-8M177 205l3-9" fill="none" stroke="var(--stripe)" strokeWidth="8" strokeLinecap="round" />
    </g>
    <g className="cat-feet">
      <ellipse className="cat-fur" cx="72" cy="225" rx="18" ry="10" />
      <ellipse className="cat-fur" cx="128" cy="225" rx="18" ry="10" />
      <path d="M65 226v5m8-5v5m48-5v5m8-5v5" fill="none" stroke="var(--stripe)" strokeWidth="1.5" />
    </g>
  </>;
}

export function CatBody({ coat }: { coat: CatCoat }) {
  return <g className="cat-body">
    <path className="cat-fur" d="M76 143C66 147 57 152 53 165Q48 180 52 193Q52 222 100 222Q148 222 148 193Q152 180 147 165C143 152 134 147 124 143Z" />
    <ellipse cx="100" cy="184" rx="27" ry="32" fill="#eadbc2" />
    <path className="tabby-stripe" d="M55 170l12 7m-13 8 11 4m81-19-12 7m13 8-12 4M73 151q27 15 54 0" />
    <path className="cat-fur" d="M48 83Q39 57 47 27Q69 31 81 53Q100 47 119 53Q133 30 153 27Q161 57 152 83Z" />
    <path d="M49 38Q65 42 71 59L49 69ZM151 38Q135 42 129 59L151 69Z" fill="#ce9991" />
    <ellipse className="cat-fur" cx="100" cy="103" rx="58" ry="52" />
    {coat === 'siamese' ? <ellipse className="point-mask" cx="100" cy="110" rx="44" ry="38" fill="var(--stripe)" /> : null}
    {coat === 'tuxedo' ? <path className="white-blaze" d="M100 65Q96 103 85 125Q100 137 115 125Q104 103 100 65Z" fill="#eadbc2" /> : null}
    <path className="tabby-stripe" d="M80 59l6 18 8-14 6 16 7-16 8 14 6-18M49 91l17 7m-20 7 18 6m89-20-17 7m20 7-18 6" />
    <ellipse cx="86" cy="132" rx="20" ry="15" fill="#eadbc2" />
    <ellipse cx="114" cy="132" rx="20" ry="15" fill="#eadbc2" />
    <path d="M93 125Q100 122 107 125L100 132Z" fill="#98676a" />
    <path d="M100 131v7" stroke="#594535" strokeWidth="2" />
    <path d="M72 126l-30-5m30 12-33 1m89-8 30-5m-30 12 33 1" stroke="#594535" strokeWidth="1.4" strokeLinecap="round" />
    <g fill="#594535">{[[77, 128], [80, 133], [123, 128], [120, 133]].map(([cx, cy]) => <circle key={`${cx}-${cy}`} cx={cx} cy={cy} r="1" />)}</g>
  </g>;
}

export function CatEyes() {
  return <>{[77, 123].map(x => <g className="eye" key={x}>
    <ellipse cx={x} cy="101" rx="18" ry="15" fill="#e5e7c7" stroke="#594535" strokeWidth="2" />
    <ellipse cx={x} cy="101" rx="13" ry="13" fill="#a6bd78" />
    <g className="pupil"><ellipse cx={x} cy="102" rx="5" ry="11" fill="#30342c" /><circle cx={x - 3} cy="97" r="3" fill="#fff" /></g>
  </g>)}</>;
}
