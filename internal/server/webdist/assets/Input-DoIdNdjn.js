import{e3 as Ie,cM as Me,e7 as Te,ev as Co,d as D,cn as te,a as m,ew as zo,$ as w,_ as k,aS as l,c8 as So,o as i,c as b,f as _o,al as v,a3 as p,ce as X,x as S,ar as oe,cd as ko,c_ as Ve,a$ as pe,d1 as Po,cc as Ao,as as A,c9 as G,P as ve,p as z,U as Ro,ex as $o,g as $,a0 as De,aH as Fo,a2 as Pe,ak as Q,a4 as Bo,ey as Eo,cr as Io,cs as Ae,k as Mo,b_ as To,aL as Re,aK as Vo,au as Do,V as $e,ct as Fe,aY as _,d0 as Be,aD as ee,F as he,cS as Lo,cp as Wo,a6 as fe,cU as Oo,W as Ko}from"./index-PJQVsA89.js";import{u as No}from"./format-length-B_v2b7Op.js";import{u as Ho}from"./use-merged-state-Ce7LuUGh.js";var Uo=/\.|\[(?:[^[\]]*|(["'])(?:(?!\1)[^\\]|\\.)*?\1)\]/,Go=/^\w*$/;function Xo(e,n){if(Ie(e))return!1;var s=typeof e;return s=="number"||s=="symbol"||s=="boolean"||e==null||Me(e)?!0:Go.test(e)||!Uo.test(e)||n!=null&&e in Object(n)}var Yo="Expected a function";function be(e,n){if(typeof e!="function"||n!=null&&typeof n!="function")throw new TypeError(Yo);var s=function(){var h=arguments,x=n?n.apply(this,h):h[0],y=s.cache;if(y.has(x))return y.get(x);var u=e.apply(this,h);return s.cache=y.set(x,u)||y,u};return s.cache=new(be.Cache||Te),s}be.Cache=Te;var Zo=500;function jo(e){var n=be(e,function(h){return s.size===Zo&&s.clear(),h}),s=n.cache;return n}var qo=/[^.[\]]+|\[(?:(-?\d+(?:\.\d+)?)|(["'])((?:(?!\2)[^\\]|\\.)*?)\2)\]|(?=(?:\.|\[\])(?:\.|\[\]|$))/g,Jo=/\\(\\)?/g,Qo=jo(function(e){var n=[];return e.charCodeAt(0)===46&&n.push(""),e.replace(qo,function(s,h,x,y){n.push(x?y.replace(Jo,"$1"):h||s)}),n});function et(e,n){return Ie(e)?e:Xo(e,n)?[e]:Qo(Co(e))}function rt(e){if(typeof e=="string"||Me(e))return e;var n=e+"";return n=="0"&&1/e==-1/0?"-0":n}function ot(e,n){n=et(n,e);for(var s=0,h=n.length;e!=null&&s<h;)e=e[rt(n[s++])];return s&&s==h?e:void 0}function zt(e,n,s){var h=e==null?void 0:ot(e,n);return h===void 0?s:h}var tt=D({name:"Eye",render(){return(()=>{const e=te("ae479a1970012861");return e[0]||(e[0]=m("svg",{xmlns:"http://www.w3.org/2000/svg",viewBox:"0 0 512 512"},[m("path",{d:"M255.66 112c-77.94 0-157.89 45.11-220.83 135.33a16 16 0 0 0-.27 17.77C82.92 340.8 161.8 400 255.66 400c92.84 0 173.34-59.38 221.79-135.25a16.14 16.14 0 0 0 0-17.47C428.89 172.28 347.8 112 255.66 112z",fill:"none",stroke:"currentColor","stroke-linecap":"round","stroke-linejoin":"round","stroke-width":"32"}),m("circle",{cx:"256",cy:"256",r:"80",fill:"none",stroke:"currentColor","stroke-miterlimit":"10","stroke-width":"32"})],-1))})()}}),nt=D({name:"EyeOff",render(){return(()=>{const e=te("2c06203b450ce879");return e[0]||(e[0]=m("svg",{xmlns:"http://www.w3.org/2000/svg",viewBox:"0 0 512 512"},[m("path",{d:"M432 448a15.92 15.92 0 0 1-11.31-4.69l-352-352a16 16 0 0 1 22.62-22.62l352 352A16 16 0 0 1 432 448z",fill:"currentColor"}),m("path",{d:"M255.66 384c-41.49 0-81.5-12.28-118.92-36.5c-34.07-22-64.74-53.51-88.7-91v-.08c19.94-28.57 41.78-52.73 65.24-72.21a2 2 0 0 0 .14-2.94L93.5 161.38a2 2 0 0 0-2.71-.12c-24.92 21-48.05 46.76-69.08 76.92a31.92 31.92 0 0 0-.64 35.54c26.41 41.33 60.4 76.14 98.28 100.65C162 402 207.9 416 255.66 416a239.13 239.13 0 0 0 75.8-12.58a2 2 0 0 0 .77-3.31l-21.58-21.58a4 4 0 0 0-3.83-1a204.8 204.8 0 0 1-51.16 6.47z",fill:"currentColor"}),m("path",{d:"M490.84 238.6c-26.46-40.92-60.79-75.68-99.27-100.53C349 110.55 302 96 255.66 96a227.34 227.34 0 0 0-74.89 12.83a2 2 0 0 0-.75 3.31l21.55 21.55a4 4 0 0 0 3.88 1a192.82 192.82 0 0 1 50.21-6.69c40.69 0 80.58 12.43 118.55 37c34.71 22.4 65.74 53.88 89.76 91a.13.13 0 0 1 0 .16a310.72 310.72 0 0 1-64.12 72.73a2 2 0 0 0-.15 2.95l19.9 19.89a2 2 0 0 0 2.7.13a343.49 343.49 0 0 0 68.64-78.48a32.2 32.2 0 0 0-.1-34.78z",fill:"currentColor"}),m("path",{d:"M256 160a95.88 95.88 0 0 0-21.37 2.4a2 2 0 0 0-1 3.38l112.59 112.56a2 2 0 0 0 3.38-1A96 96 0 0 0 256 160z",fill:"currentColor"}),m("path",{d:"M165.78 233.66a2 2 0 0 0-3.38 1a96 96 0 0 0 115 115a2 2 0 0 0 1-3.38z",fill:"currentColor"})],-1))})()}}),at=zo("clear",()=>(()=>{const e=te("c93f8499adf26ca3");return e[0]||(e[0]=m("svg",{viewBox:"0 0 16 16",version:"1.1",xmlns:"http://www.w3.org/2000/svg"},[m("g",{stroke:"none","stroke-width":"1",fill:"none","fill-rule":"evenodd"},[m("g",{fill:"currentColor","fill-rule":"nonzero"},[m("path",{d:"M8,2 C11.3137085,2 14,4.6862915 14,8 C14,11.3137085 11.3137085,14 8,14 C4.6862915,14 2,11.3137085 2,8 C2,4.6862915 4.6862915,2 8,2 Z M6.5343055,5.83859116 C6.33943736,5.70359511 6.07001296,5.72288026 5.89644661,5.89644661 L5.89644661,5.89644661 L5.83859116,5.9656945 C5.70359511,6.16056264 5.72288026,6.42998704 5.89644661,6.60355339 L5.89644661,6.60355339 L7.293,8 L5.89644661,9.39644661 L5.83859116,9.4656945 C5.70359511,9.66056264 5.72288026,9.92998704 5.89644661,10.1035534 L5.89644661,10.1035534 L5.9656945,10.1614088 C6.16056264,10.2964049 6.42998704,10.2771197 6.60355339,10.1035534 L6.60355339,10.1035534 L8,8.707 L9.39644661,10.1035534 L9.4656945,10.1614088 C9.66056264,10.2964049 9.92998704,10.2771197 10.1035534,10.1035534 L10.1035534,10.1035534 L10.1614088,10.0343055 C10.2964049,9.83943736 10.2771197,9.57001296 10.1035534,9.39644661 L10.1035534,9.39644661 L8.707,8 L10.1035534,6.60355339 L10.1614088,6.5343055 C10.2964049,6.33943736 10.2771197,6.07001296 10.1035534,5.89644661 L10.1035534,5.89644661 L10.0343055,5.83859116 C9.83943736,5.70359511 9.57001296,5.72288026 9.39644661,5.89644661 L9.39644661,5.89644661 L8,7.293 L6.60355339,5.89644661 Z"})])])],-1))})()),lt=w("base-clear",`
 flex-shrink: 0;
 height: 1em;
 width: 1em;
 position: relative;
`,[k(">",[l("clear",`
 font-size: var(--n-clear-size);
 height: 1em;
 width: 1em;
 cursor: pointer;
 color: var(--n-clear-color);
 transition: color .3s var(--n-bezier);
 display: flex;
 `,[k("&:hover",`
 color: var(--n-clear-color-hover)!important;
 `),k("&:active",`
 color: var(--n-clear-color-pressed)!important;
 `)]),l("placeholder",`
 display: flex;
 `),l("clear, placeholder",`
 position: absolute;
 left: 50%;
 top: 50%;
 transform: translateX(-50%) translateY(-50%);
 `,[So({originalTransform:"translateX(-50%) translateY(-50%)",left:"50%",top:"50%"})])])]);const it=["onClick","onMousedown"];var ge=D({name:"BaseClear",props:{clsPrefix:{type:String,required:!0},show:Boolean,onClear:Function},setup(e){return Ve("-base-clear",lt,pe(e,"clsPrefix")),{handleMouseDown(n){n.preventDefault()}}},render(){const{clsPrefix:e}=this;return i(),b("div",{class:v(`${e}-base-clear`)},[_o(ko,null,{default:()=>this.show?(i(),b("div",{key:"dismiss",class:v(`${e}-base-clear__clear`),onClick:this.onClear,onMousedown:this.handleMouseDown,"data-clear":!0},[p(()=>X(this.$slots.icon,()=>[(i(),S(oe,{clsPrefix:e},{default:()=>(i(),S(at))},1032,["clsPrefix"]))]))],42,it)):(i(),b("div",{key:"icon",class:v(`${e}-base-clear__placeholder`)},[p(()=>this.$slots.placeholder?.())],2))},1024)],2)}}),st=D({name:"ChevronDown",render(){return(()=>{const e=te("ae90ecf811a811ac");return e[0]||(e[0]=m("svg",{viewBox:"0 0 16 16",fill:"none",xmlns:"http://www.w3.org/2000/svg"},[m("path",{d:"M3.14645 5.64645C3.34171 5.45118 3.65829 5.45118 3.85355 5.64645L8 9.79289L12.1464 5.64645C12.3417 5.45118 12.6583 5.45118 12.8536 5.64645C13.0488 5.84171 13.0488 6.15829 12.8536 6.35355L8.35355 10.8536C8.15829 11.0488 7.84171 11.0488 7.64645 10.8536L3.14645 6.35355C2.95118 6.15829 2.95118 5.84171 3.14645 5.64645Z",fill:"currentColor"})],-1))})()}}),ct=D({name:"InternalSelectionSuffix",props:{clsPrefix:{type:String,required:!0},showArrow:{type:Boolean,default:void 0},showClear:{type:Boolean,default:void 0},loading:Boolean,onClear:Function},setup(e,{slots:n}){return()=>{const{clsPrefix:s}=e;return i(),S(Po,{clsPrefix:s,class:v(`${s}-base-suffix`),strokeWidth:24,scale:.85,show:e.loading},{default:()=>e.showArrow?(i(),S(ge,{key:1,clsPrefix:s,show:e.showClear,onClear:e.onClear},{placeholder:()=>(i(),S(oe,{clsPrefix:s,class:v(`${s}-base-suffix__arrow`)},{default:()=>X(n.default,()=>[(i(),S(st))])},1032,["clsPrefix","class"]))},1032,["clsPrefix","show","onClear"])):null},1032,["clsPrefix","class","show"])}}});const Le=Ao("n-input");var ut=w("input",`
 max-width: 100%;
 cursor: text;
 line-height: 1.5;
 z-index: auto;
 outline: none;
 box-sizing: border-box;
 position: relative;
 display: inline-flex;
 border-radius: var(--n-border-radius);
 background-color: var(--n-color);
 transition: background-color .3s var(--n-bezier);
 font-size: var(--n-font-size);
 font-weight: var(--n-font-weight);
 --n-padding-vertical: calc((var(--n-height) - 1.5 * var(--n-font-size)) / 2);
`,[l("input, textarea",`
 overflow: hidden;
 flex-grow: 1;
 position: relative;
 `),l("input-el, textarea-el, input-mirror, textarea-mirror, separator, placeholder",`
 box-sizing: border-box;
 font-size: inherit;
 line-height: 1.5;
 font-family: inherit;
 border: none;
 outline: none;
 background-color: #0000;
 text-align: inherit;
 transition:
 -webkit-text-fill-color .3s var(--n-bezier),
 caret-color .3s var(--n-bezier),
 color .3s var(--n-bezier),
 text-decoration-color .3s var(--n-bezier);
 `),l("input-el, textarea-el",`
 -webkit-appearance: none;
 scrollbar-width: none;
 width: 100%;
 min-width: 0;
 text-decoration-color: var(--n-text-decoration-color);
 color: var(--n-text-color);
 caret-color: var(--n-caret-color);
 background-color: transparent;
 `,[k("&::-webkit-scrollbar, &::-webkit-scrollbar-track-piece, &::-webkit-scrollbar-thumb",`
 width: 0;
 height: 0;
 display: none;
 `),k("&::placeholder",`
 color: #0000;
 -webkit-text-fill-color: transparent !important;
 `),k("&:-webkit-autofill ~",[l("placeholder","display: none;")])]),A("round",[G("textarea","border-radius: calc(var(--n-height) / 2);")]),l("placeholder",`
 pointer-events: none;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 overflow: hidden;
 color: var(--n-placeholder-color);
 `,[k("span",`
 width: 100%;
 display: inline-block;
 `)]),A("textarea",[l("placeholder","overflow: visible;")]),G("autosize","width: 100%;"),A("autosize",[l("textarea-el, input-el",`
 position: absolute;
 top: 0;
 left: 0;
 height: 100%;
 `)]),w("input-wrapper",`
 overflow: hidden;
 display: inline-flex;
 flex-grow: 1;
 position: relative;
 padding-left: var(--n-padding-left);
 padding-right: var(--n-padding-right);
 `),l("input-mirror",`
 padding: 0;
 height: var(--n-height);
 line-height: var(--n-height);
 overflow: hidden;
 visibility: hidden;
 position: static;
 white-space: pre;
 pointer-events: none;
 `),l("input-el",`
 padding: 0;
 height: var(--n-height);
 line-height: var(--n-height);
 `,[k("&[type=password]::-ms-reveal","display: none;"),k("+",[l("placeholder",`
 display: flex;
 align-items: center; 
 `)])]),G("textarea",[l("placeholder","white-space: nowrap;")]),l("eye",`
 display: flex;
 align-items: center;
 justify-content: center;
 transition: color .3s var(--n-bezier);
 `),A("textarea","width: 100%;",[w("input-word-count",`
 position: absolute;
 right: var(--n-padding-right);
 bottom: var(--n-padding-vertical);
 `),A("resizable",[w("input-wrapper",`
 resize: vertical;
 min-height: var(--n-height);
 `)]),l("textarea-el, textarea-mirror, placeholder",`
 height: 100%;
 padding-left: 0;
 padding-right: 0;
 padding-top: var(--n-padding-vertical);
 padding-bottom: var(--n-padding-vertical);
 word-break: break-word;
 display: inline-block;
 vertical-align: bottom;
 box-sizing: border-box;
 line-height: var(--n-line-height-textarea);
 margin: 0;
 resize: none;
 white-space: pre-wrap;
 scroll-padding-block-end: var(--n-padding-vertical);
 `),l("textarea-mirror",`
 width: 100%;
 pointer-events: none;
 overflow: hidden;
 visibility: hidden;
 position: static;
 white-space: pre-wrap;
 overflow-wrap: break-word;
 `)]),A("pair",[l("input-el, placeholder","text-align: center;"),l("separator",`
 display: flex;
 align-items: center;
 transition: color .3s var(--n-bezier);
 color: var(--n-text-color);
 white-space: nowrap;
 `,[w("icon",`
 color: var(--n-icon-color);
 `),w("base-icon",`
 color: var(--n-icon-color);
 `)])]),A("disabled",`
 cursor: not-allowed;
 background-color: var(--n-color-disabled);
 `,[l("border","border: var(--n-border-disabled);"),l("input-el, textarea-el",`
 cursor: not-allowed;
 color: var(--n-text-color-disabled);
 text-decoration-color: var(--n-text-color-disabled);
 `),l("placeholder","color: var(--n-placeholder-color-disabled);"),l("separator","color: var(--n-text-color-disabled);",[w("icon",`
 color: var(--n-icon-color-disabled);
 `),w("base-icon",`
 color: var(--n-icon-color-disabled);
 `)]),w("input-word-count",`
 color: var(--n-count-text-color-disabled);
 `),l("suffix, prefix","color: var(--n-text-color-disabled);",[w("icon",`
 color: var(--n-icon-color-disabled);
 `),w("internal-icon",`
 color: var(--n-icon-color-disabled);
 `)])]),G("disabled",[l("eye",`
 color: var(--n-icon-color);
 cursor: pointer;
 `,[k("&:hover",`
 color: var(--n-icon-color-hover);
 `),k("&:active",`
 color: var(--n-icon-color-pressed);
 `)]),k("&:hover","background-color: var(--n-color-hover);",[l("state-border","border: var(--n-border-hover);")]),A("focus","background-color: var(--n-color-focus);",[l("state-border",`
 border: var(--n-border-focus);
 box-shadow: var(--n-box-shadow-focus);
 `)])]),l("border, state-border",`
 box-sizing: border-box;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 pointer-events: none;
 border-radius: inherit;
 border: var(--n-border);
 transition:
 box-shadow .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `),l("state-border",`
 border-color: #0000;
 z-index: 1;
 `),l("prefix","margin-right: 4px;"),l("suffix",`
 margin-left: 4px;
 `),l("suffix, prefix",`
 transition: color .3s var(--n-bezier);
 flex-wrap: nowrap;
 flex-shrink: 0;
 line-height: var(--n-height);
 white-space: nowrap;
 display: inline-flex;
 align-items: center;
 justify-content: center;
 color: var(--n-suffix-text-color);
 `,[w("base-loading",`
 font-size: var(--n-icon-size);
 margin: 0 2px;
 color: var(--n-loading-color);
 `),w("base-clear",`
 font-size: var(--n-icon-size);
 `,[l("placeholder",[w("base-icon",`
 transition: color .3s var(--n-bezier);
 color: var(--n-icon-color);
 font-size: var(--n-icon-size);
 `)])]),k(">",[w("icon",`
 transition: color .3s var(--n-bezier);
 color: var(--n-icon-color);
 font-size: var(--n-icon-size);
 `)]),w("base-icon",`
 font-size: var(--n-icon-size);
 `)]),w("input-word-count",`
 pointer-events: none;
 line-height: 1.5;
 font-size: .85em;
 color: var(--n-count-text-color);
 transition: color .3s var(--n-bezier);
 margin-left: 4px;
 font-variant: tabular-nums;
 `),["warning","error"].map(e=>A(`${e}-status`,[G("disabled",[w("base-loading",`
 color: var(--n-loading-color-${e})
 `),l("input-el, textarea-el",`
 caret-color: var(--n-caret-color-${e});
 `),l("state-border",`
 border: var(--n-border-${e});
 `),k("&:hover",[l("state-border",`
 border: var(--n-border-hover-${e});
 `)]),k("&:focus",`
 background-color: var(--n-color-focus-${e});
 `,[l("state-border",`
 box-shadow: var(--n-box-shadow-focus-${e});
 border: var(--n-border-focus-${e});
 `)]),A("focus",`
 background-color: var(--n-color-focus-${e});
 `,[l("state-border",`
 box-shadow: var(--n-box-shadow-focus-${e});
 border: var(--n-border-focus-${e});
 `)])])]))]);const dt=w("input",[A("disabled",[l("input-el, textarea-el",`
 -webkit-text-fill-color: var(--n-text-color-disabled);
 `)])]);function ht(e){let n=0;for(const s of e)n++;return n}function re(e){return e===""||e==null}function ft(e){const n=z(null);function s(){const{value:y}=e;if(!y?.focus){x();return}const{selectionStart:u,selectionEnd:t,value:c}=y;if(u==null||t==null){x();return}n.value={start:u,end:t,beforeText:c.slice(0,u),afterText:c.slice(t)}}function h(){const{value:y}=n,{value:u}=e;if(!y||!u)return;const{value:t}=u,{start:c,beforeText:P,afterText:O}=y;let C=t.length;if(t.endsWith(O))C=t.length-O.length;else if(t.startsWith(P))C=P.length;else{const M=P[c-1],F=t.indexOf(M,c-1);F!==-1&&(C=F+1)}u.setSelectionRange?.(C,C)}function x(){n.value=null}return ve(e,x),{recordCursor:s,restoreCursor:h}}var Ee=D({name:"InputWordCount",setup(e,{slots:n}){const{mergedValueRef:s,maxlengthRef:h,mergedClsPrefixRef:x,countGraphemesRef:y}=Ro(Le),u=$(()=>{const{value:t}=s;return t===null||Array.isArray(t)?0:(y.value||ht)(t)});return()=>{const{value:t}=h,{value:c}=s;return i(),b("span",{class:v(`${x.value}-input-word-count`)},[p(()=>$o(n.default,{value:c===null||Array.isArray(c)?"":c},()=>[t===void 0?u.value:`${u.value} / ${t}`]))],2)}}});const pt=["autofocus","rows","placeholder","value","disabled","maxlength","minlength","readonly","tabindex","onBlur","onFocus","onInput","onChange","onScroll"],vt=["type","tabindex","placeholder","disabled","maxlength","minlength","value","readonly","autofocus","size","onBlur","onFocus","onInput","onChange"],gt=["onMousedown","onClick"],bt=["type","tabindex","placeholder","disabled","maxlength","minlength","value","readonly","onBlur","onFocus","onInput","onChange"],mt=["tabindex","onFocus","onBlur","onClick","onMousedown","onMouseenter","onMouseleave","onCompositionstart","onCompositionend","onKeyup","onKeydown"],xt={...De.props,bordered:{type:Boolean,default:void 0},type:{type:String,default:"text"},placeholder:[Array,String],defaultValue:{type:[String,Array],default:null},value:[String,Array],disabled:{type:Boolean,default:void 0},size:String,rows:{type:[Number,String],default:3},round:Boolean,minlength:[String,Number],maxlength:[String,Number],clearable:Boolean,autosize:{type:[Boolean,Object],default:!1},pair:Boolean,separator:String,readonly:{type:[String,Boolean],default:!1},passivelyActivated:Boolean,showPasswordOn:String,stateful:{type:Boolean,default:!0},autofocus:Boolean,inputProps:Object,resizable:{type:Boolean,default:!0},showCount:Boolean,loading:{type:Boolean,default:void 0},allowInput:Function,renderCount:Function,onMousedown:Function,onKeydown:Function,onKeyup:[Function,Array],onInput:[Function,Array],onFocus:[Function,Array],onBlur:[Function,Array],onClick:[Function,Array],onChange:[Function,Array],onClear:[Function,Array],countGraphemes:Function,status:String,"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],textDecoration:[String,Array],attrSize:{type:Number,default:20},onInputBlur:[Function,Array],onInputFocus:[Function,Array],onDeactivate:[Function,Array],onActivate:[Function,Array],onWrapperFocus:[Function,Array],onWrapperBlur:[Function,Array],internalDeactivateOnEnter:Boolean,internalForceFocus:Boolean,internalLoadingBeforeSuffix:{type:Boolean,default:!0},showPasswordToggle:Boolean};var St=D({name:"Input",props:xt,slots:Object,setup(e){const{mergedClsPrefixRef:n,mergedBorderedRef:s,inlineThemeDisabled:h,mergedRtlRef:x,mergedComponentPropsRef:y}=Bo(e),u=De("Input","-input",ut,Wo,e,n);Eo&&Ve("-input-safari",dt,n);const t=z(null),c=z(null),P=z(null),O=z(null),C=z(null),M=z(null),F=z(null),me=ft(F),T=z(null),{localeRef:We}=No("Input"),Y=z(e.defaultValue),Oe=pe(e,"value"),R=Ho(Oe,Y),K=Io(e,{mergedSize:r=>{const{size:o}=e;if(o)return o;const{mergedSize:a}=r||{};if(a?.value)return a.value;const f=y?.value?.Input?.size;return f||"medium"}}),{mergedSizeRef:ne,mergedDisabledRef:L,mergedStatusRef:Ke}=K,W=z(!1),N=z(!1),B=z(!1),H=z(!1);let ae=null;const le=$(()=>{const{placeholder:r,pair:o}=e;return o?Array.isArray(r)?r:r===void 0?["",""]:[r,r]:r===void 0?[We.value.placeholder]:[r]}),Ne=$(()=>{const{value:r}=B,{value:o}=R,{value:a}=le;return!r&&(re(o)||Array.isArray(o)&&re(o[0]))&&a[0]}),He=$(()=>{const{value:r}=B,{value:o}=R,{value:a}=le;return!r&&a[1]&&(re(o)||Array.isArray(o)&&re(o[1]))}),ie=Ae(()=>e.internalForceFocus||W.value),Ue=Ae(()=>{if(L.value||e.readonly||!e.clearable||!ie.value&&!N.value)return!1;const{value:r}=R,{value:o}=ie;return e.pair?!!(Array.isArray(r)&&(r[0]||r[1]))&&(N.value||o):!!r&&(N.value||o)}),se=$(()=>{const{showPasswordOn:r}=e;if(r)return r;if(e.showPasswordToggle)return"click"}),U=z(!1),Ge=$(()=>{const{textDecoration:r}=e;return r?Array.isArray(r)?r.map(o=>({textDecoration:o})):[{textDecoration:r}]:["",""]}),xe=z(void 0),Xe=()=>{if(e.type==="textarea"){const{autosize:r}=e;if(r&&(xe.value=T.value?.$el?.offsetWidth),!c.value||typeof r=="boolean")return;const{paddingTop:o,paddingBottom:a,lineHeight:f}=window.getComputedStyle(c.value),g=Number(o.slice(0,-2)),d=Number(a.slice(0,-2)),V=Number(f.slice(0,-2)),{value:E}=P;if(!E)return;if(r.minRows){const I=Math.max(r.minRows,1),de=`${g+d+V*I}px`;E.style.minHeight=de}if(r.maxRows){const I=`${g+d+V*r.maxRows}px`;E.style.maxHeight=I}}},Ye=$(()=>{const{maxlength:r}=e;return r===void 0?void 0:Number(r)});Mo(()=>{const{value:r}=R;Array.isArray(r)||ue(r)});const Ze=To().proxy;function Z(r,o){const{onUpdateValue:a,"onUpdate:value":f,onInput:g}=e,{nTriggerFormInput:d}=K;a&&_(a,r,o),f&&_(f,r,o),g&&_(g,r,o),Y.value=r,d()}function j(r,o){const{onChange:a}=e,{nTriggerFormChange:f}=K;a&&_(a,r,o),Y.value=r,f()}function je(r){const{onBlur:o}=e,{nTriggerFormBlur:a}=K;o&&_(o,r),a()}function qe(r){const{onFocus:o}=e,{nTriggerFormFocus:a}=K;o&&_(o,r),a()}function Je(r){const{onClear:o}=e;o&&_(o,r)}function Qe(r){const{onInputBlur:o}=e;o&&_(o,r)}function er(r){const{onInputFocus:o}=e;o&&_(o,r)}function rr(){const{onDeactivate:r}=e;r&&_(r)}function or(){const{onActivate:r}=e;r&&_(r)}function tr(r){const{onClick:o}=e;o&&_(o,r)}function nr(r){const{onWrapperFocus:o}=e;o&&_(o,r)}function ar(r){const{onWrapperBlur:o}=e;o&&_(o,r)}function lr(){B.value=!0}function ir(r){B.value=!1,r.target===M.value?q(r,1):q(r,0)}function q(r,o=0,a="input"){const f=r.target.value;if(ue(f),r instanceof InputEvent&&!r.isComposing&&(B.value=!1),e.type==="textarea"){const{value:d}=T;d&&d.syncUnifiedContainer()}if(ae=f,B.value)return;me.recordCursor();const g=sr(f);if(g)if(!e.pair)a==="input"?Z(f,{source:o}):j(f,{source:o});else{let{value:d}=R;Array.isArray(d)?d=[d[0],d[1]]:d=["",""],d[o]=f,a==="input"?Z(d,{source:o}):j(d,{source:o})}Ze.$forceUpdate(),g||$e(me.restoreCursor)}function sr(r){const{countGraphemes:o,maxlength:a,minlength:f}=e;if(o){let d;if(a!==void 0&&(d===void 0&&(d=o(r)),d>Number(a))||f!==void 0&&(d===void 0&&(d=o(r)),d<Number(a)))return!1}const{allowInput:g}=e;return typeof g=="function"?g(r):!0}function cr(r){Qe(r),r.relatedTarget===t.value&&rr(),r.relatedTarget!==null&&(r.relatedTarget===C.value||r.relatedTarget===M.value||r.relatedTarget===c.value)||(H.value=!1),J(r,"blur"),F.value=null}function ur(r,o){er(r),W.value=!0,H.value=!0,or(),J(r,"focus"),o===0?F.value=C.value:o===1?F.value=M.value:o===2&&(F.value=c.value)}function dr(r){e.passivelyActivated&&(ar(r),J(r,"blur"))}function hr(r){e.passivelyActivated&&(W.value=!0,nr(r),J(r,"focus"))}function J(r,o){r.relatedTarget!==null&&(r.relatedTarget===C.value||r.relatedTarget===M.value||r.relatedTarget===c.value||r.relatedTarget===t.value)||(o==="focus"?(qe(r),W.value=!0):o==="blur"&&(je(r),W.value=!1))}function fr(r,o){q(r,o,"change")}function pr(r){tr(r)}function vr(r){Je(r),we()}function we(){e.pair?(Z(["",""],{source:"clear"}),j(["",""],{source:"clear"})):(Z("",{source:"clear"}),j("",{source:"clear"}))}function gr(r){const{onMousedown:o}=e;o&&o(r);const{tagName:a}=r.target;if(a!=="INPUT"&&a!=="TEXTAREA"){if(e.resizable){const{value:f}=t;if(f){const{left:g,top:d,width:V,height:E}=f.getBoundingClientRect(),I=14;if(g+V-I<r.clientX&&r.clientX<g+V&&d+E-I<r.clientY&&r.clientY<d+E)return}}r.preventDefault(),W.value||ye()}}function br(){N.value=!0,e.type==="textarea"&&T.value?.handleMouseEnterWrapper()}function mr(){N.value=!1,e.type==="textarea"&&T.value?.handleMouseLeaveWrapper()}function xr(){L.value||se.value==="click"&&(U.value=!U.value)}function wr(r){if(L.value)return;r.preventDefault();const o=f=>{f.preventDefault(),Be("mouseup",document,o)};if(Fe("mouseup",document,o),se.value!=="mousedown")return;U.value=!0;const a=()=>{U.value=!1,Be("mouseup",document,a)};Fe("mouseup",document,a)}function yr(r){e.onKeyup&&_(e.onKeyup,r)}function Cr(r){switch(e.onKeydown&&_(e.onKeydown,r),r.key){case"Escape":ce();break;case"Enter":zr(r)}}function zr(r){if(e.passivelyActivated){const{value:o}=H;if(o){e.internalDeactivateOnEnter&&ce();return}r.preventDefault(),e.type==="textarea"?c.value?.focus():C.value?.focus()}}function ce(){e.passivelyActivated&&(H.value=!1,$e(()=>{t.value?.focus()}))}function ye(){L.value||(e.passivelyActivated?t.value?.focus():(c.value?.focus(),C.value?.focus()))}function Sr(){t.value?.contains(document.activeElement)&&document.activeElement.blur()}function _r(){c.value?.select(),C.value?.select()}function kr(){L.value||(c.value?c.value.focus():C.value&&C.value.focus())}function Pr(){const{value:r}=t;r?.contains(document.activeElement)&&r!==document.activeElement&&ce()}function Ar(r){if(e.type==="textarea"){const{value:o}=c;o?.scrollTo(r)}else{const{value:o}=C;o?.scrollTo(r)}}function ue(r){const{type:o,pair:a,autosize:f}=e;if(!a&&f)if(o==="textarea"){const{value:g}=P;g&&(g.textContent=`${r??""}\r
`)}else{const{value:g}=O;g&&(r?g.textContent=r:g.innerHTML="&nbsp;")}}function Rr(){Xe()}const Ce=z({top:"0"});function $r(r){const{scrollTop:o}=r.target;Ce.value.top=`${-o}px`,T.value?.syncUnifiedContainer()}let ze=null;Re(()=>{const{autosize:r,type:o}=e;r&&o==="textarea"?ze=ve(R,a=>{!Array.isArray(a)&&a!==ae&&ue(a)}):ze?.()});let Se=null;Re(()=>{e.type==="textarea"?Se=ve(R,r=>{!Array.isArray(r)&&r!==ae&&T.value?.syncUnifiedContainer()}):Se?.()}),Ko(Le,{mergedValueRef:R,maxlengthRef:Ye,mergedClsPrefixRef:n,countGraphemesRef:pe(e,"countGraphemes")});const Fr={wrapperElRef:t,inputElRef:C,textareaElRef:c,isCompositing:B,clear:we,focus:ye,blur:Sr,select:_r,deactivate:Pr,activate:kr,scrollTo:Ar},Br=Vo("Input",x,n),_e=$(()=>{const{value:r}=ne,{common:{cubicBezierEaseInOut:o},self:{color:a,colorHover:f,borderRadius:g,textColor:d,caretColor:V,caretColorError:E,caretColorWarning:I,textDecorationColor:de,border:Er,borderDisabled:Ir,borderHover:Mr,borderFocus:Tr,placeholderColor:Vr,placeholderColorDisabled:Dr,lineHeightTextarea:Lr,colorDisabled:Wr,colorFocus:Or,textColorDisabled:Kr,boxShadowFocus:Nr,iconSize:Hr,colorFocusWarning:Ur,boxShadowFocusWarning:Gr,borderWarning:Xr,borderFocusWarning:Yr,borderHoverWarning:Zr,colorFocusError:jr,boxShadowFocusError:qr,borderError:Jr,borderFocusError:Qr,borderHoverError:eo,clearSize:ro,clearColor:oo,clearColorHover:to,clearColorPressed:no,iconColor:ao,iconColorDisabled:lo,suffixTextColor:io,countTextColor:so,countTextColorDisabled:co,iconColorHover:uo,iconColorPressed:ho,loadingColor:fo,loadingColorError:po,loadingColorWarning:vo,fontWeight:go,[fe("padding",r)]:bo,[fe("fontSize",r)]:mo,[fe("height",r)]:xo}}=u.value,{left:wo,right:yo}=Oo(bo);return{"--n-bezier":o,"--n-count-text-color":so,"--n-count-text-color-disabled":co,"--n-color":a,"--n-color-hover":f,"--n-font-size":mo,"--n-font-weight":go,"--n-border-radius":g,"--n-height":xo,"--n-padding-left":wo,"--n-padding-right":yo,"--n-text-color":d,"--n-caret-color":V,"--n-text-decoration-color":de,"--n-border":Er,"--n-border-disabled":Ir,"--n-border-hover":Mr,"--n-border-focus":Tr,"--n-placeholder-color":Vr,"--n-placeholder-color-disabled":Dr,"--n-icon-size":Hr,"--n-line-height-textarea":Lr,"--n-color-disabled":Wr,"--n-color-focus":Or,"--n-text-color-disabled":Kr,"--n-box-shadow-focus":Nr,"--n-loading-color":fo,"--n-caret-color-warning":I,"--n-color-focus-warning":Ur,"--n-box-shadow-focus-warning":Gr,"--n-border-warning":Xr,"--n-border-focus-warning":Yr,"--n-border-hover-warning":Zr,"--n-loading-color-warning":vo,"--n-caret-color-error":E,"--n-color-focus-error":jr,"--n-box-shadow-focus-error":qr,"--n-border-error":Jr,"--n-border-focus-error":Qr,"--n-border-hover-error":eo,"--n-loading-color-error":po,"--n-clear-color":oo,"--n-clear-size":ro,"--n-clear-color-hover":to,"--n-clear-color-pressed":no,"--n-icon-color":ao,"--n-icon-color-hover":uo,"--n-icon-color-pressed":ho,"--n-icon-color-disabled":lo,"--n-suffix-text-color":io}}),ke=h?Do("input",$(()=>{const{value:r}=ne;return r[0]}),_e,e):void 0;return{...Fr,wrapperElRef:t,inputElRef:C,inputMirrorElRef:O,inputEl2Ref:M,textareaElRef:c,textareaMirrorElRef:P,textareaScrollbarInstRef:T,rtlEnabled:Br,uncontrolledValue:Y,mergedValue:R,passwordVisible:U,mergedPlaceholder:le,showPlaceholder1:Ne,showPlaceholder2:He,mergedFocus:ie,isComposing:B,activated:H,showClearButton:Ue,mergedSize:ne,mergedDisabled:L,textDecorationStyle:Ge,mergedClsPrefix:n,mergedBordered:s,mergedShowPasswordOn:se,placeholderStyle:Ce,mergedStatus:Ke,textAreaScrollContainerWidth:xe,handleTextAreaScroll:$r,handleCompositionStart:lr,handleCompositionEnd:ir,handleInput:q,handleInputBlur:cr,handleInputFocus:ur,handleWrapperBlur:dr,handleWrapperFocus:hr,handleMouseEnter:br,handleMouseLeave:mr,handleMouseDown:gr,handleChange:fr,handleClick:pr,handleClear:vr,handlePasswordToggleClick:xr,handlePasswordToggleMousedown:wr,handleWrapperKeydown:Cr,handleWrapperKeyup:yr,handleTextAreaMirrorResize:Rr,getTextareaScrollContainer:()=>c.value,mergedTheme:u,cssVars:h?void 0:_e,themeClass:ke?.themeClass,onRender:ke?.onRender}},render(){const{mergedClsPrefix:e,mergedStatus:n,themeClass:s,type:h,countGraphemes:x,onRender:y}=this,u=this.$slots;return y?.(),i(),b("div",{ref:"wrapperElRef",class:v([`${e}-input`,`${e}-input--${this.mergedSize}-size`,s,n&&`${e}-input--${n}-status`,{[`${e}-input--rtl`]:this.rtlEnabled,[`${e}-input--disabled`]:this.mergedDisabled,[`${e}-input--textarea`]:h==="textarea",[`${e}-input--resizable`]:this.resizable&&!this.autosize,[`${e}-input--autosize`]:this.autosize,[`${e}-input--round`]:this.round&&h!=="textarea",[`${e}-input--pair`]:this.pair,[`${e}-input--focus`]:this.mergedFocus,[`${e}-input--stateful`]:this.stateful}]),style:Q(this.cssVars),tabindex:!this.mergedDisabled&&this.passivelyActivated&&!this.activated?0:void 0,onFocus:this.handleWrapperFocus,onBlur:this.handleWrapperBlur,onClick:this.handleClick,onMousedown:this.handleMouseDown,onMouseenter:this.handleMouseEnter,onMouseleave:this.handleMouseLeave,onCompositionstart:this.handleCompositionStart,onCompositionend:this.handleCompositionEnd,onKeyup:this.handleWrapperKeyup,onKeydown:this.handleWrapperKeydown},[m("div",{class:v(`${e}-input-wrapper`)},[p(()=>ee(u.prefix,t=>t&&(i(),b("div",{class:v(`${e}-input__prefix`)},[p(()=>t)],2)))),h==="textarea"?(i(),S(Fo,{key:0,ref:"textareaScrollbarInstRef",class:v(`${e}-input__textarea`),container:this.getTextareaScrollContainer,theme:this.theme?.peers?.Scrollbar,themeOverrides:this.themeOverrides?.peers?.Scrollbar,triggerDisplayManually:!0,useUnifiedContainer:!0,internalHoistYRail:!0},{default:()=>{const{textAreaScrollContainerWidth:t}=this,c={width:this.autosize&&t&&`${t}px`};return i(),b(he,null,[m("textarea",Pe(this.inputProps,{ref:"textareaElRef",class:[`${e}-input__textarea-el`,this.inputProps?.class],autofocus:this.autofocus,rows:Number(this.rows),placeholder:this.placeholder,value:this.mergedValue,disabled:this.mergedDisabled,maxlength:x?void 0:this.maxlength,minlength:x?void 0:this.minlength,readonly:this.readonly,tabindex:this.passivelyActivated&&!this.activated?-1:void 0,style:[this.textDecorationStyle[0],this.inputProps?.style,c],onBlur:this.handleInputBlur,onFocus:P=>{this.handleInputFocus(P,2)},onInput:this.handleInput,onChange:this.handleChange,onScroll:this.handleTextAreaScroll}),null,16,pt),this.showPlaceholder1?(i(),b("div",{class:v(`${e}-input__placeholder`),style:Q([this.placeholderStyle,c]),key:"placeholder"},[p(()=>this.mergedPlaceholder[0])],6)):p(()=>null),this.autosize?(i(),S(Lo,{key:2,onResize:this.handleTextAreaMirrorResize},{default:()=>(i(),b("div",{ref:"textareaMirrorElRef",class:v(`${e}-input__textarea-mirror`),key:"mirror"},null,2))},1032,["onResize"])):p(()=>null)],64)}},1032,["class","container","theme","themeOverrides"])):(i(),b("div",{key:1,class:v(`${e}-input__input`)},[m("input",Pe({type:h==="password"&&this.mergedShowPasswordOn&&this.passwordVisible?"text":h},this.inputProps,{ref:"inputElRef",class:[`${e}-input__input-el`,this.inputProps?.class],style:[this.textDecorationStyle[0],this.inputProps?.style],tabindex:this.passivelyActivated&&!this.activated?-1:this.inputProps?.tabindex,placeholder:this.mergedPlaceholder[0],disabled:this.mergedDisabled,maxlength:x?void 0:this.maxlength,minlength:x?void 0:this.minlength,value:Array.isArray(this.mergedValue)?this.mergedValue[0]:this.mergedValue,readonly:this.readonly,autofocus:this.autofocus,size:this.attrSize,onBlur:this.handleInputBlur,onFocus:t=>{this.handleInputFocus(t,0)},onInput:t=>{this.handleInput(t,0)},onChange:t=>{this.handleChange(t,0)}}),null,16,vt),this.showPlaceholder1?(i(),b("div",{key:0,class:v(`${e}-input__placeholder`)},[m("span",null,[p(()=>this.mergedPlaceholder[0])])],2)):p(()=>null),this.autosize?(i(),b("div",{class:v(`${e}-input__input-mirror`),key:"mirror",ref:"inputMirrorElRef"}," ",2)):p(()=>null)],2)),p(()=>!this.pair&&ee(u.suffix,t=>t||this.clearable||this.showCount||this.mergedShowPasswordOn||this.loading!==void 0?(i(),b("div",{key:1,class:v(`${e}-input__suffix`)},[p(()=>[ee(u["clear-icon-placeholder"],c=>(this.clearable||c)&&(i(),S(ge,{clsPrefix:e,show:this.showClearButton,onClear:this.handleClear},{placeholder:()=>c,icon:()=>this.$slots["clear-icon"]?.()},1032,["clsPrefix","show","onClear"]))),this.internalLoadingBeforeSuffix?null:t,this.loading!==void 0?(i(),S(ct,{key:2,clsPrefix:e,loading:this.loading,showArrow:!1,showClear:!1,style:Q(this.cssVars)},null,8,["clsPrefix","loading","style"])):null,this.internalLoadingBeforeSuffix?t:null,this.showCount&&this.type!=="textarea"?(i(),S(Ee,{key:3},{default:c=>{const{renderCount:P}=this;return P?P(c):u.count?.(c)}},1024)):null,this.mergedShowPasswordOn&&this.type==="password"?(i(),b("div",{key:4,class:v(`${e}-input__eye`),onMousedown:this.handlePasswordToggleMousedown,onClick:this.handlePasswordToggleClick},[this.passwordVisible?(i(),b(he,{key:0},[p(()=>X(u["password-visible-icon"],()=>[(i(),S(oe,{clsPrefix:e},{default:()=>(i(),S(tt))},1032,["clsPrefix"]))]))],64)):(i(),b(he,{key:1},[p(()=>X(u["password-invisible-icon"],()=>[(i(),S(oe,{clsPrefix:e},{default:()=>(i(),S(nt))},1032,["clsPrefix"]))]))],64))],42,gt)):null])],2)):null))],2),this.pair?(i(),b("span",{key:0,class:v(`${e}-input__separator`)},[p(()=>X(u.separator,()=>[this.separator]))],2)):p(()=>null),this.pair?(i(),b("div",{key:2,class:v(`${e}-input-wrapper`)},[m("div",{class:v(`${e}-input__input`)},[m("input",{ref:"inputEl2Ref",type:this.type,class:v(`${e}-input__input-el`),tabindex:this.passivelyActivated&&!this.activated?-1:void 0,placeholder:this.mergedPlaceholder[1],disabled:this.mergedDisabled,maxlength:x?void 0:this.maxlength,minlength:x?void 0:this.minlength,value:Array.isArray(this.mergedValue)?this.mergedValue[1]:void 0,readonly:this.readonly,style:Q(this.textDecorationStyle[1]),onBlur:this.handleInputBlur,onFocus:t=>{this.handleInputFocus(t,1)},onInput:t=>{this.handleInput(t,1)},onChange:t=>{this.handleChange(t,1)}},null,46,bt),this.showPlaceholder2?(i(),b("div",{key:0,class:v(`${e}-input__placeholder`)},[m("span",null,[p(()=>this.mergedPlaceholder[1])])],2)):p(()=>null)],2),p(()=>ee(u.suffix,t=>(this.clearable||t)&&(i(),b("div",{class:v(`${e}-input__suffix`)},[p(()=>[this.clearable&&(i(),S(ge,{clsPrefix:e,show:this.showClearButton,onClear:this.handleClear},{icon:()=>u["clear-icon"]?.(),placeholder:()=>u["clear-icon-placeholder"]?.()},1032,["clsPrefix","show","onClear"])),t])],2))))],2)):p(()=>null),this.mergedBordered?(i(),b("div",{key:4,class:v(`${e}-input__border`)},null,2)):p(()=>null),this.mergedBordered?(i(),b("div",{key:6,class:v(`${e}-input__state-border`)},null,2)):p(()=>null),this.showCount&&h==="textarea"?(i(),S(Ee,{key:8},{default:t=>{const{renderCount:c}=this;return c?c(t):u.count?.(t)}},1024)):p(()=>null)],46,mt)}});export{st as C,St as I,ct as S,ot as b,et as c,zt as g,Xo as i,rt as t};
