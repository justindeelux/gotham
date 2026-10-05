import{dt as Ie,cf as Me,dx as Te,dV as Co,K as oe,z as F,d as W,bU as ne,a as m,dW as zo,W as w,V as k,aJ as i,bI as So,o as s,c as b,e as _o,ad as v,_ as p,bO as G,n as S,aj as te,bN as ko,ct as Ve,aS as ve,cw as Po,bM as Ao,ak as A,bJ as X,s as z,P as Fo,dX as $o,X as We,ay as Bo,Z as Pe,ac as Q,$ as Ro,dY as Eo,bY as Io,bZ as Ae,g as Mo,bx as To,aC as Fe,aB as Vo,am as Wo,Q as $e,b_ as Be,aP as _,cv as Re,ar as ee,F as fe,cl as Do,bW as Lo,a1 as pe,cn as Oo,S as Ko}from"./index-CUvFmr9Z.js";import{u as No}from"./format-length-0lhWs8Va.js";var Ho=/\.|\[(?:[^[\]]*|(["'])(?:(?!\1)[^\\]|\\.)*?\1)\]/,Uo=/^\w*$/;function Xo(e,n){if(Ie(e))return!1;var a=typeof e;return a=="number"||a=="symbol"||a=="boolean"||e==null||Me(e)?!0:Uo.test(e)||!Ho.test(e)||n!=null&&e in Object(n)}var Go="Expected a function";function be(e,n){if(typeof e!="function"||n!=null&&typeof n!="function")throw new TypeError(Go);var a=function(){var h=arguments,x=n?n.apply(this,h):h[0],y=a.cache;if(y.has(x))return y.get(x);var u=e.apply(this,h);return a.cache=y.set(x,u)||y,u};return a.cache=new(be.Cache||Te),a}be.Cache=Te;var Yo=500;function Zo(e){var n=be(e,function(h){return a.size===Yo&&a.clear(),h}),a=n.cache;return n}var jo=/[^.[\]]+|\[(?:(-?\d+(?:\.\d+)?)|(["'])((?:(?!\2)[^\\]|\\.)*?)\2)\]|(?=(?:\.|\[\])(?:\.|\[\]|$))/g,qo=/\\(\\)?/g,Jo=Zo(function(e){var n=[];return e.charCodeAt(0)===46&&n.push(""),e.replace(jo,function(a,h,x,y){n.push(x?y.replace(qo,"$1"):h||a)}),n});function Qo(e,n){return Ie(e)?e:Xo(e,n)?[e]:Jo(Co(e))}function et(e){if(typeof e=="string"||Me(e))return e;var n=e+"";return n=="0"&&1/e==-1/0?"-0":n}function rt(e,n){n=Qo(n,e);for(var a=0,h=n.length;e!=null&&a<h;)e=e[et(n[a++])];return a&&a==h?e:void 0}function Ct(e,n,a){var h=e==null?void 0:rt(e,n);return h===void 0?a:h}function ot(e,n){return oe(e,a=>{a!==void 0&&(n.value=a)}),F(()=>e.value===void 0?n.value:e.value)}var tt=W({name:"Eye",render(){return(()=>{const e=ne("ae479a1970012861");return e[0]||(e[0]=m("svg",{xmlns:"http://www.w3.org/2000/svg",viewBox:"0 0 512 512"},[m("path",{d:"M255.66 112c-77.94 0-157.89 45.11-220.83 135.33a16 16 0 0 0-.27 17.77C82.92 340.8 161.8 400 255.66 400c92.84 0 173.34-59.38 221.79-135.25a16.14 16.14 0 0 0 0-17.47C428.89 172.28 347.8 112 255.66 112z",fill:"none",stroke:"currentColor","stroke-linecap":"round","stroke-linejoin":"round","stroke-width":"32"}),m("circle",{cx:"256",cy:"256",r:"80",fill:"none",stroke:"currentColor","stroke-miterlimit":"10","stroke-width":"32"})],-1))})()}}),nt=W({name:"EyeOff",render(){return(()=>{const e=ne("2c06203b450ce879");return e[0]||(e[0]=m("svg",{xmlns:"http://www.w3.org/2000/svg",viewBox:"0 0 512 512"},[m("path",{d:"M432 448a15.92 15.92 0 0 1-11.31-4.69l-352-352a16 16 0 0 1 22.62-22.62l352 352A16 16 0 0 1 432 448z",fill:"currentColor"}),m("path",{d:"M255.66 384c-41.49 0-81.5-12.28-118.92-36.5c-34.07-22-64.74-53.51-88.7-91v-.08c19.94-28.57 41.78-52.73 65.24-72.21a2 2 0 0 0 .14-2.94L93.5 161.38a2 2 0 0 0-2.71-.12c-24.92 21-48.05 46.76-69.08 76.92a31.92 31.92 0 0 0-.64 35.54c26.41 41.33 60.4 76.14 98.28 100.65C162 402 207.9 416 255.66 416a239.13 239.13 0 0 0 75.8-12.58a2 2 0 0 0 .77-3.31l-21.58-21.58a4 4 0 0 0-3.83-1a204.8 204.8 0 0 1-51.16 6.47z",fill:"currentColor"}),m("path",{d:"M490.84 238.6c-26.46-40.92-60.79-75.68-99.27-100.53C349 110.55 302 96 255.66 96a227.34 227.34 0 0 0-74.89 12.83a2 2 0 0 0-.75 3.31l21.55 21.55a4 4 0 0 0 3.88 1a192.82 192.82 0 0 1 50.21-6.69c40.69 0 80.58 12.43 118.55 37c34.71 22.4 65.74 53.88 89.76 91a.13.13 0 0 1 0 .16a310.72 310.72 0 0 1-64.12 72.73a2 2 0 0 0-.15 2.95l19.9 19.89a2 2 0 0 0 2.7.13a343.49 343.49 0 0 0 68.64-78.48a32.2 32.2 0 0 0-.1-34.78z",fill:"currentColor"}),m("path",{d:"M256 160a95.88 95.88 0 0 0-21.37 2.4a2 2 0 0 0-1 3.38l112.59 112.56a2 2 0 0 0 3.38-1A96 96 0 0 0 256 160z",fill:"currentColor"}),m("path",{d:"M165.78 233.66a2 2 0 0 0-3.38 1a96 96 0 0 0 115 115a2 2 0 0 0 1-3.38z",fill:"currentColor"})],-1))})()}}),at=zo("clear",()=>(()=>{const e=ne("c93f8499adf26ca3");return e[0]||(e[0]=m("svg",{viewBox:"0 0 16 16",version:"1.1",xmlns:"http://www.w3.org/2000/svg"},[m("g",{stroke:"none","stroke-width":"1",fill:"none","fill-rule":"evenodd"},[m("g",{fill:"currentColor","fill-rule":"nonzero"},[m("path",{d:"M8,2 C11.3137085,2 14,4.6862915 14,8 C14,11.3137085 11.3137085,14 8,14 C4.6862915,14 2,11.3137085 2,8 C2,4.6862915 4.6862915,2 8,2 Z M6.5343055,5.83859116 C6.33943736,5.70359511 6.07001296,5.72288026 5.89644661,5.89644661 L5.89644661,5.89644661 L5.83859116,5.9656945 C5.70359511,6.16056264 5.72288026,6.42998704 5.89644661,6.60355339 L5.89644661,6.60355339 L7.293,8 L5.89644661,9.39644661 L5.83859116,9.4656945 C5.70359511,9.66056264 5.72288026,9.92998704 5.89644661,10.1035534 L5.89644661,10.1035534 L5.9656945,10.1614088 C6.16056264,10.2964049 6.42998704,10.2771197 6.60355339,10.1035534 L6.60355339,10.1035534 L8,8.707 L9.39644661,10.1035534 L9.4656945,10.1614088 C9.66056264,10.2964049 9.92998704,10.2771197 10.1035534,10.1035534 L10.1035534,10.1035534 L10.1614088,10.0343055 C10.2964049,9.83943736 10.2771197,9.57001296 10.1035534,9.39644661 L10.1035534,9.39644661 L8.707,8 L10.1035534,6.60355339 L10.1614088,6.5343055 C10.2964049,6.33943736 10.2771197,6.07001296 10.1035534,5.89644661 L10.1035534,5.89644661 L10.0343055,5.83859116 C9.83943736,5.70359511 9.57001296,5.72288026 9.39644661,5.89644661 L9.39644661,5.89644661 L8,7.293 L6.60355339,5.89644661 Z"})])])],-1))})()),lt=w("base-clear",`
 flex-shrink: 0;
 height: 1em;
 width: 1em;
 position: relative;
`,[k(">",[i("clear",`
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
 `)]),i("placeholder",`
 display: flex;
 `),i("clear, placeholder",`
 position: absolute;
 left: 50%;
 top: 50%;
 transform: translateX(-50%) translateY(-50%);
 `,[So({originalTransform:"translateX(-50%) translateY(-50%)",left:"50%",top:"50%"})])])]);const it=["onClick","onMousedown"];var ge=W({name:"BaseClear",props:{clsPrefix:{type:String,required:!0},show:Boolean,onClear:Function},setup(e){return Ve("-base-clear",lt,ve(e,"clsPrefix")),{handleMouseDown(n){n.preventDefault()}}},render(){const{clsPrefix:e}=this;return s(),b("div",{class:v(`${e}-base-clear`)},[_o(ko,null,{default:()=>this.show?(s(),b("div",{key:"dismiss",class:v(`${e}-base-clear__clear`),onClick:this.onClear,onMousedown:this.handleMouseDown,"data-clear":!0},[p(()=>G(this.$slots.icon,()=>[(s(),S(te,{clsPrefix:e},{default:()=>(s(),S(at))},1032,["clsPrefix"]))]))],42,it)):(s(),b("div",{key:"icon",class:v(`${e}-base-clear__placeholder`)},[p(()=>this.$slots.placeholder?.())],2))},1024)],2)}}),st=W({name:"ChevronDown",render(){return(()=>{const e=ne("ae90ecf811a811ac");return e[0]||(e[0]=m("svg",{viewBox:"0 0 16 16",fill:"none",xmlns:"http://www.w3.org/2000/svg"},[m("path",{d:"M3.14645 5.64645C3.34171 5.45118 3.65829 5.45118 3.85355 5.64645L8 9.79289L12.1464 5.64645C12.3417 5.45118 12.6583 5.45118 12.8536 5.64645C13.0488 5.84171 13.0488 6.15829 12.8536 6.35355L8.35355 10.8536C8.15829 11.0488 7.84171 11.0488 7.64645 10.8536L3.14645 6.35355C2.95118 6.15829 2.95118 5.84171 3.14645 5.64645Z",fill:"currentColor"})],-1))})()}}),ct=W({name:"InternalSelectionSuffix",props:{clsPrefix:{type:String,required:!0},showArrow:{type:Boolean,default:void 0},showClear:{type:Boolean,default:void 0},loading:Boolean,onClear:Function},setup(e,{slots:n}){return()=>{const{clsPrefix:a}=e;return s(),S(Po,{clsPrefix:a,class:v(`${a}-base-suffix`),strokeWidth:24,scale:.85,show:e.loading},{default:()=>e.showArrow?(s(),S(ge,{key:1,clsPrefix:a,show:e.showClear,onClear:e.onClear},{placeholder:()=>(s(),S(te,{clsPrefix:a,class:v(`${a}-base-suffix__arrow`)},{default:()=>G(n.default,()=>[(s(),S(st))])},1032,["clsPrefix","class"]))},1032,["clsPrefix","show","onClear"])):null},1032,["clsPrefix","class","show"])}}});const De=Ao("n-input");var ut=w("input",`
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
`,[i("input, textarea",`
 overflow: hidden;
 flex-grow: 1;
 position: relative;
 `),i("input-el, textarea-el, input-mirror, textarea-mirror, separator, placeholder",`
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
 `),i("input-el, textarea-el",`
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
 `),k("&:-webkit-autofill ~",[i("placeholder","display: none;")])]),A("round",[X("textarea","border-radius: calc(var(--n-height) / 2);")]),i("placeholder",`
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
 `)]),A("textarea",[i("placeholder","overflow: visible;")]),X("autosize","width: 100%;"),A("autosize",[i("textarea-el, input-el",`
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
 `),i("input-mirror",`
 padding: 0;
 height: var(--n-height);
 line-height: var(--n-height);
 overflow: hidden;
 visibility: hidden;
 position: static;
 white-space: pre;
 pointer-events: none;
 `),i("input-el",`
 padding: 0;
 height: var(--n-height);
 line-height: var(--n-height);
 `,[k("&[type=password]::-ms-reveal","display: none;"),k("+",[i("placeholder",`
 display: flex;
 align-items: center; 
 `)])]),X("textarea",[i("placeholder","white-space: nowrap;")]),i("eye",`
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
 `)]),i("textarea-el, textarea-mirror, placeholder",`
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
 `),i("textarea-mirror",`
 width: 100%;
 pointer-events: none;
 overflow: hidden;
 visibility: hidden;
 position: static;
 white-space: pre-wrap;
 overflow-wrap: break-word;
 `)]),A("pair",[i("input-el, placeholder","text-align: center;"),i("separator",`
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
 `,[i("border","border: var(--n-border-disabled);"),i("input-el, textarea-el",`
 cursor: not-allowed;
 color: var(--n-text-color-disabled);
 text-decoration-color: var(--n-text-color-disabled);
 `),i("placeholder","color: var(--n-placeholder-color-disabled);"),i("separator","color: var(--n-text-color-disabled);",[w("icon",`
 color: var(--n-icon-color-disabled);
 `),w("base-icon",`
 color: var(--n-icon-color-disabled);
 `)]),w("input-word-count",`
 color: var(--n-count-text-color-disabled);
 `),i("suffix, prefix","color: var(--n-text-color-disabled);",[w("icon",`
 color: var(--n-icon-color-disabled);
 `),w("internal-icon",`
 color: var(--n-icon-color-disabled);
 `)])]),X("disabled",[i("eye",`
 color: var(--n-icon-color);
 cursor: pointer;
 `,[k("&:hover",`
 color: var(--n-icon-color-hover);
 `),k("&:active",`
 color: var(--n-icon-color-pressed);
 `)]),k("&:hover","background-color: var(--n-color-hover);",[i("state-border","border: var(--n-border-hover);")]),A("focus","background-color: var(--n-color-focus);",[i("state-border",`
 border: var(--n-border-focus);
 box-shadow: var(--n-box-shadow-focus);
 `)])]),i("border, state-border",`
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
 `),i("state-border",`
 border-color: #0000;
 z-index: 1;
 `),i("prefix","margin-right: 4px;"),i("suffix",`
 margin-left: 4px;
 `),i("suffix, prefix",`
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
 `,[i("placeholder",[w("base-icon",`
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
 `),["warning","error"].map(e=>A(`${e}-status`,[X("disabled",[w("base-loading",`
 color: var(--n-loading-color-${e})
 `),i("input-el, textarea-el",`
 caret-color: var(--n-caret-color-${e});
 `),i("state-border",`
 border: var(--n-border-${e});
 `),k("&:hover",[i("state-border",`
 border: var(--n-border-hover-${e});
 `)]),k("&:focus",`
 background-color: var(--n-color-focus-${e});
 `,[i("state-border",`
 box-shadow: var(--n-box-shadow-focus-${e});
 border: var(--n-border-focus-${e});
 `)]),A("focus",`
 background-color: var(--n-color-focus-${e});
 `,[i("state-border",`
 box-shadow: var(--n-box-shadow-focus-${e});
 border: var(--n-border-focus-${e});
 `)])])]))]);const dt=w("input",[A("disabled",[i("input-el, textarea-el",`
 -webkit-text-fill-color: var(--n-text-color-disabled);
 `)])]);function ht(e){let n=0;for(const a of e)n++;return n}function re(e){return e===""||e==null}function ft(e){const n=z(null);function a(){const{value:y}=e;if(!y?.focus){x();return}const{selectionStart:u,selectionEnd:t,value:c}=y;if(u==null||t==null){x();return}n.value={start:u,end:t,beforeText:c.slice(0,u),afterText:c.slice(t)}}function h(){const{value:y}=n,{value:u}=e;if(!y||!u)return;const{value:t}=u,{start:c,beforeText:P,afterText:O}=y;let C=t.length;if(t.endsWith(O))C=t.length-O.length;else if(t.startsWith(P))C=P.length;else{const M=P[c-1],B=t.indexOf(M,c-1);B!==-1&&(C=B+1)}u.setSelectionRange?.(C,C)}function x(){n.value=null}return oe(e,x),{recordCursor:a,restoreCursor:h}}var Ee=W({name:"InputWordCount",setup(e,{slots:n}){const{mergedValueRef:a,maxlengthRef:h,mergedClsPrefixRef:x,countGraphemesRef:y}=Fo(De),u=F(()=>{const{value:t}=a;return t===null||Array.isArray(t)?0:(y.value||ht)(t)});return()=>{const{value:t}=h,{value:c}=a;return s(),b("span",{class:v(`${x.value}-input-word-count`)},[p(()=>$o(n.default,{value:c===null||Array.isArray(c)?"":c},()=>[t===void 0?u.value:`${u.value} / ${t}`]))],2)}}});const pt=["autofocus","rows","placeholder","value","disabled","maxlength","minlength","readonly","tabindex","onBlur","onFocus","onInput","onChange","onScroll"],vt=["type","tabindex","placeholder","disabled","maxlength","minlength","value","readonly","autofocus","size","onBlur","onFocus","onInput","onChange"],gt=["onMousedown","onClick"],bt=["type","tabindex","placeholder","disabled","maxlength","minlength","value","readonly","onBlur","onFocus","onInput","onChange"],mt=["tabindex","onFocus","onBlur","onClick","onMousedown","onMouseenter","onMouseleave","onCompositionstart","onCompositionend","onKeyup","onKeydown"],xt={...We.props,bordered:{type:Boolean,default:void 0},type:{type:String,default:"text"},placeholder:[Array,String],defaultValue:{type:[String,Array],default:null},value:[String,Array],disabled:{type:Boolean,default:void 0},size:String,rows:{type:[Number,String],default:3},round:Boolean,minlength:[String,Number],maxlength:[String,Number],clearable:Boolean,autosize:{type:[Boolean,Object],default:!1},pair:Boolean,separator:String,readonly:{type:[String,Boolean],default:!1},passivelyActivated:Boolean,showPasswordOn:String,stateful:{type:Boolean,default:!0},autofocus:Boolean,inputProps:Object,resizable:{type:Boolean,default:!0},showCount:Boolean,loading:{type:Boolean,default:void 0},allowInput:Function,renderCount:Function,onMousedown:Function,onKeydown:Function,onKeyup:[Function,Array],onInput:[Function,Array],onFocus:[Function,Array],onBlur:[Function,Array],onClick:[Function,Array],onChange:[Function,Array],onClear:[Function,Array],countGraphemes:Function,status:String,"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],textDecoration:[String,Array],attrSize:{type:Number,default:20},onInputBlur:[Function,Array],onInputFocus:[Function,Array],onDeactivate:[Function,Array],onActivate:[Function,Array],onWrapperFocus:[Function,Array],onWrapperBlur:[Function,Array],internalDeactivateOnEnter:Boolean,internalForceFocus:Boolean,internalLoadingBeforeSuffix:{type:Boolean,default:!0},showPasswordToggle:Boolean};var zt=W({name:"Input",props:xt,slots:Object,setup(e){const{mergedClsPrefixRef:n,mergedBorderedRef:a,inlineThemeDisabled:h,mergedRtlRef:x,mergedComponentPropsRef:y}=Ro(e),u=We("Input","-input",ut,Lo,e,n);Eo&&Ve("-input-safari",dt,n);const t=z(null),c=z(null),P=z(null),O=z(null),C=z(null),M=z(null),B=z(null),me=ft(B),T=z(null),{localeRef:Le}=No("Input"),Y=z(e.defaultValue),Oe=ve(e,"value"),$=ot(Oe,Y),K=Io(e,{mergedSize:r=>{const{size:o}=e;if(o)return o;const{mergedSize:l}=r||{};if(l?.value)return l.value;const f=y?.value?.Input?.size;return f||"medium"}}),{mergedSizeRef:ae,mergedDisabledRef:D,mergedStatusRef:Ke}=K,L=z(!1),N=z(!1),R=z(!1),H=z(!1);let le=null;const ie=F(()=>{const{placeholder:r,pair:o}=e;return o?Array.isArray(r)?r:r===void 0?["",""]:[r,r]:r===void 0?[Le.value.placeholder]:[r]}),Ne=F(()=>{const{value:r}=R,{value:o}=$,{value:l}=ie;return!r&&(re(o)||Array.isArray(o)&&re(o[0]))&&l[0]}),He=F(()=>{const{value:r}=R,{value:o}=$,{value:l}=ie;return!r&&l[1]&&(re(o)||Array.isArray(o)&&re(o[1]))}),se=Ae(()=>e.internalForceFocus||L.value),Ue=Ae(()=>{if(D.value||e.readonly||!e.clearable||!se.value&&!N.value)return!1;const{value:r}=$,{value:o}=se;return e.pair?!!(Array.isArray(r)&&(r[0]||r[1]))&&(N.value||o):!!r&&(N.value||o)}),ce=F(()=>{const{showPasswordOn:r}=e;if(r)return r;if(e.showPasswordToggle)return"click"}),U=z(!1),Xe=F(()=>{const{textDecoration:r}=e;return r?Array.isArray(r)?r.map(o=>({textDecoration:o})):[{textDecoration:r}]:["",""]}),xe=z(void 0),Ge=()=>{if(e.type==="textarea"){const{autosize:r}=e;if(r&&(xe.value=T.value?.$el?.offsetWidth),!c.value||typeof r=="boolean")return;const{paddingTop:o,paddingBottom:l,lineHeight:f}=window.getComputedStyle(c.value),g=Number(o.slice(0,-2)),d=Number(l.slice(0,-2)),V=Number(f.slice(0,-2)),{value:E}=P;if(!E)return;if(r.minRows){const I=Math.max(r.minRows,1),he=`${g+d+V*I}px`;E.style.minHeight=he}if(r.maxRows){const I=`${g+d+V*r.maxRows}px`;E.style.maxHeight=I}}},Ye=F(()=>{const{maxlength:r}=e;return r===void 0?void 0:Number(r)});Mo(()=>{const{value:r}=$;Array.isArray(r)||de(r)});const Ze=To().proxy;function Z(r,o){const{onUpdateValue:l,"onUpdate:value":f,onInput:g}=e,{nTriggerFormInput:d}=K;l&&_(l,r,o),f&&_(f,r,o),g&&_(g,r,o),Y.value=r,d()}function j(r,o){const{onChange:l}=e,{nTriggerFormChange:f}=K;l&&_(l,r,o),Y.value=r,f()}function je(r){const{onBlur:o}=e,{nTriggerFormBlur:l}=K;o&&_(o,r),l()}function qe(r){const{onFocus:o}=e,{nTriggerFormFocus:l}=K;o&&_(o,r),l()}function Je(r){const{onClear:o}=e;o&&_(o,r)}function Qe(r){const{onInputBlur:o}=e;o&&_(o,r)}function er(r){const{onInputFocus:o}=e;o&&_(o,r)}function rr(){const{onDeactivate:r}=e;r&&_(r)}function or(){const{onActivate:r}=e;r&&_(r)}function tr(r){const{onClick:o}=e;o&&_(o,r)}function nr(r){const{onWrapperFocus:o}=e;o&&_(o,r)}function ar(r){const{onWrapperBlur:o}=e;o&&_(o,r)}function lr(){R.value=!0}function ir(r){R.value=!1,r.target===M.value?q(r,1):q(r,0)}function q(r,o=0,l="input"){const f=r.target.value;if(de(f),r instanceof InputEvent&&!r.isComposing&&(R.value=!1),e.type==="textarea"){const{value:d}=T;d&&d.syncUnifiedContainer()}if(le=f,R.value)return;me.recordCursor();const g=sr(f);if(g)if(!e.pair)l==="input"?Z(f,{source:o}):j(f,{source:o});else{let{value:d}=$;Array.isArray(d)?d=[d[0],d[1]]:d=["",""],d[o]=f,l==="input"?Z(d,{source:o}):j(d,{source:o})}Ze.$forceUpdate(),g||$e(me.restoreCursor)}function sr(r){const{countGraphemes:o,maxlength:l,minlength:f}=e;if(o){let d;if(l!==void 0&&(d===void 0&&(d=o(r)),d>Number(l))||f!==void 0&&(d===void 0&&(d=o(r)),d<Number(l)))return!1}const{allowInput:g}=e;return typeof g=="function"?g(r):!0}function cr(r){Qe(r),r.relatedTarget===t.value&&rr(),r.relatedTarget!==null&&(r.relatedTarget===C.value||r.relatedTarget===M.value||r.relatedTarget===c.value)||(H.value=!1),J(r,"blur"),B.value=null}function ur(r,o){er(r),L.value=!0,H.value=!0,or(),J(r,"focus"),o===0?B.value=C.value:o===1?B.value=M.value:o===2&&(B.value=c.value)}function dr(r){e.passivelyActivated&&(ar(r),J(r,"blur"))}function hr(r){e.passivelyActivated&&(L.value=!0,nr(r),J(r,"focus"))}function J(r,o){r.relatedTarget!==null&&(r.relatedTarget===C.value||r.relatedTarget===M.value||r.relatedTarget===c.value||r.relatedTarget===t.value)||(o==="focus"?(qe(r),L.value=!0):o==="blur"&&(je(r),L.value=!1))}function fr(r,o){q(r,o,"change")}function pr(r){tr(r)}function vr(r){Je(r),we()}function we(){e.pair?(Z(["",""],{source:"clear"}),j(["",""],{source:"clear"})):(Z("",{source:"clear"}),j("",{source:"clear"}))}function gr(r){const{onMousedown:o}=e;o&&o(r);const{tagName:l}=r.target;if(l!=="INPUT"&&l!=="TEXTAREA"){if(e.resizable){const{value:f}=t;if(f){const{left:g,top:d,width:V,height:E}=f.getBoundingClientRect(),I=14;if(g+V-I<r.clientX&&r.clientX<g+V&&d+E-I<r.clientY&&r.clientY<d+E)return}}r.preventDefault(),L.value||ye()}}function br(){N.value=!0,e.type==="textarea"&&T.value?.handleMouseEnterWrapper()}function mr(){N.value=!1,e.type==="textarea"&&T.value?.handleMouseLeaveWrapper()}function xr(){D.value||ce.value==="click"&&(U.value=!U.value)}function wr(r){if(D.value)return;r.preventDefault();const o=f=>{f.preventDefault(),Re("mouseup",document,o)};if(Be("mouseup",document,o),ce.value!=="mousedown")return;U.value=!0;const l=()=>{U.value=!1,Re("mouseup",document,l)};Be("mouseup",document,l)}function yr(r){e.onKeyup&&_(e.onKeyup,r)}function Cr(r){switch(e.onKeydown&&_(e.onKeydown,r),r.key){case"Escape":ue();break;case"Enter":zr(r)}}function zr(r){if(e.passivelyActivated){const{value:o}=H;if(o){e.internalDeactivateOnEnter&&ue();return}r.preventDefault(),e.type==="textarea"?c.value?.focus():C.value?.focus()}}function ue(){e.passivelyActivated&&(H.value=!1,$e(()=>{t.value?.focus()}))}function ye(){D.value||(e.passivelyActivated?t.value?.focus():(c.value?.focus(),C.value?.focus()))}function Sr(){t.value?.contains(document.activeElement)&&document.activeElement.blur()}function _r(){c.value?.select(),C.value?.select()}function kr(){D.value||(c.value?c.value.focus():C.value&&C.value.focus())}function Pr(){const{value:r}=t;r?.contains(document.activeElement)&&r!==document.activeElement&&ue()}function Ar(r){if(e.type==="textarea"){const{value:o}=c;o?.scrollTo(r)}else{const{value:o}=C;o?.scrollTo(r)}}function de(r){const{type:o,pair:l,autosize:f}=e;if(!l&&f)if(o==="textarea"){const{value:g}=P;g&&(g.textContent=`${r??""}\r
`)}else{const{value:g}=O;g&&(r?g.textContent=r:g.innerHTML="&nbsp;")}}function Fr(){Ge()}const Ce=z({top:"0"});function $r(r){const{scrollTop:o}=r.target;Ce.value.top=`${-o}px`,T.value?.syncUnifiedContainer()}let ze=null;Fe(()=>{const{autosize:r,type:o}=e;r&&o==="textarea"?ze=oe($,l=>{!Array.isArray(l)&&l!==le&&de(l)}):ze?.()});let Se=null;Fe(()=>{e.type==="textarea"?Se=oe($,r=>{!Array.isArray(r)&&r!==le&&T.value?.syncUnifiedContainer()}):Se?.()}),Ko(De,{mergedValueRef:$,maxlengthRef:Ye,mergedClsPrefixRef:n,countGraphemesRef:ve(e,"countGraphemes")});const Br={wrapperElRef:t,inputElRef:C,textareaElRef:c,isCompositing:R,clear:we,focus:ye,blur:Sr,select:_r,deactivate:Pr,activate:kr,scrollTo:Ar},Rr=Vo("Input",x,n),_e=F(()=>{const{value:r}=ae,{common:{cubicBezierEaseInOut:o},self:{color:l,colorHover:f,borderRadius:g,textColor:d,caretColor:V,caretColorError:E,caretColorWarning:I,textDecorationColor:he,border:Er,borderDisabled:Ir,borderHover:Mr,borderFocus:Tr,placeholderColor:Vr,placeholderColorDisabled:Wr,lineHeightTextarea:Dr,colorDisabled:Lr,colorFocus:Or,textColorDisabled:Kr,boxShadowFocus:Nr,iconSize:Hr,colorFocusWarning:Ur,boxShadowFocusWarning:Xr,borderWarning:Gr,borderFocusWarning:Yr,borderHoverWarning:Zr,colorFocusError:jr,boxShadowFocusError:qr,borderError:Jr,borderFocusError:Qr,borderHoverError:eo,clearSize:ro,clearColor:oo,clearColorHover:to,clearColorPressed:no,iconColor:ao,iconColorDisabled:lo,suffixTextColor:io,countTextColor:so,countTextColorDisabled:co,iconColorHover:uo,iconColorPressed:ho,loadingColor:fo,loadingColorError:po,loadingColorWarning:vo,fontWeight:go,[pe("padding",r)]:bo,[pe("fontSize",r)]:mo,[pe("height",r)]:xo}}=u.value,{left:wo,right:yo}=Oo(bo);return{"--n-bezier":o,"--n-count-text-color":so,"--n-count-text-color-disabled":co,"--n-color":l,"--n-color-hover":f,"--n-font-size":mo,"--n-font-weight":go,"--n-border-radius":g,"--n-height":xo,"--n-padding-left":wo,"--n-padding-right":yo,"--n-text-color":d,"--n-caret-color":V,"--n-text-decoration-color":he,"--n-border":Er,"--n-border-disabled":Ir,"--n-border-hover":Mr,"--n-border-focus":Tr,"--n-placeholder-color":Vr,"--n-placeholder-color-disabled":Wr,"--n-icon-size":Hr,"--n-line-height-textarea":Dr,"--n-color-disabled":Lr,"--n-color-focus":Or,"--n-text-color-disabled":Kr,"--n-box-shadow-focus":Nr,"--n-loading-color":fo,"--n-caret-color-warning":I,"--n-color-focus-warning":Ur,"--n-box-shadow-focus-warning":Xr,"--n-border-warning":Gr,"--n-border-focus-warning":Yr,"--n-border-hover-warning":Zr,"--n-loading-color-warning":vo,"--n-caret-color-error":E,"--n-color-focus-error":jr,"--n-box-shadow-focus-error":qr,"--n-border-error":Jr,"--n-border-focus-error":Qr,"--n-border-hover-error":eo,"--n-loading-color-error":po,"--n-clear-color":oo,"--n-clear-size":ro,"--n-clear-color-hover":to,"--n-clear-color-pressed":no,"--n-icon-color":ao,"--n-icon-color-hover":uo,"--n-icon-color-pressed":ho,"--n-icon-color-disabled":lo,"--n-suffix-text-color":io}}),ke=h?Wo("input",F(()=>{const{value:r}=ae;return r[0]}),_e,e):void 0;return{...Br,wrapperElRef:t,inputElRef:C,inputMirrorElRef:O,inputEl2Ref:M,textareaElRef:c,textareaMirrorElRef:P,textareaScrollbarInstRef:T,rtlEnabled:Rr,uncontrolledValue:Y,mergedValue:$,passwordVisible:U,mergedPlaceholder:ie,showPlaceholder1:Ne,showPlaceholder2:He,mergedFocus:se,isComposing:R,activated:H,showClearButton:Ue,mergedSize:ae,mergedDisabled:D,textDecorationStyle:Xe,mergedClsPrefix:n,mergedBordered:a,mergedShowPasswordOn:ce,placeholderStyle:Ce,mergedStatus:Ke,textAreaScrollContainerWidth:xe,handleTextAreaScroll:$r,handleCompositionStart:lr,handleCompositionEnd:ir,handleInput:q,handleInputBlur:cr,handleInputFocus:ur,handleWrapperBlur:dr,handleWrapperFocus:hr,handleMouseEnter:br,handleMouseLeave:mr,handleMouseDown:gr,handleChange:fr,handleClick:pr,handleClear:vr,handlePasswordToggleClick:xr,handlePasswordToggleMousedown:wr,handleWrapperKeydown:Cr,handleWrapperKeyup:yr,handleTextAreaMirrorResize:Fr,getTextareaScrollContainer:()=>c.value,mergedTheme:u,cssVars:h?void 0:_e,themeClass:ke?.themeClass,onRender:ke?.onRender}},render(){const{mergedClsPrefix:e,mergedStatus:n,themeClass:a,type:h,countGraphemes:x,onRender:y}=this,u=this.$slots;return y?.(),s(),b("div",{ref:"wrapperElRef",class:v([`${e}-input`,`${e}-input--${this.mergedSize}-size`,a,n&&`${e}-input--${n}-status`,{[`${e}-input--rtl`]:this.rtlEnabled,[`${e}-input--disabled`]:this.mergedDisabled,[`${e}-input--textarea`]:h==="textarea",[`${e}-input--resizable`]:this.resizable&&!this.autosize,[`${e}-input--autosize`]:this.autosize,[`${e}-input--round`]:this.round&&h!=="textarea",[`${e}-input--pair`]:this.pair,[`${e}-input--focus`]:this.mergedFocus,[`${e}-input--stateful`]:this.stateful}]),style:Q(this.cssVars),tabindex:!this.mergedDisabled&&this.passivelyActivated&&!this.activated?0:void 0,onFocus:this.handleWrapperFocus,onBlur:this.handleWrapperBlur,onClick:this.handleClick,onMousedown:this.handleMouseDown,onMouseenter:this.handleMouseEnter,onMouseleave:this.handleMouseLeave,onCompositionstart:this.handleCompositionStart,onCompositionend:this.handleCompositionEnd,onKeyup:this.handleWrapperKeyup,onKeydown:this.handleWrapperKeydown},[m("div",{class:v(`${e}-input-wrapper`)},[p(()=>ee(u.prefix,t=>t&&(s(),b("div",{class:v(`${e}-input__prefix`)},[p(()=>t)],2)))),h==="textarea"?(s(),S(Bo,{key:0,ref:"textareaScrollbarInstRef",class:v(`${e}-input__textarea`),container:this.getTextareaScrollContainer,theme:this.theme?.peers?.Scrollbar,themeOverrides:this.themeOverrides?.peers?.Scrollbar,triggerDisplayManually:!0,useUnifiedContainer:!0,internalHoistYRail:!0},{default:()=>{const{textAreaScrollContainerWidth:t}=this,c={width:this.autosize&&t&&`${t}px`};return s(),b(fe,null,[m("textarea",Pe(this.inputProps,{ref:"textareaElRef",class:[`${e}-input__textarea-el`,this.inputProps?.class],autofocus:this.autofocus,rows:Number(this.rows),placeholder:this.placeholder,value:this.mergedValue,disabled:this.mergedDisabled,maxlength:x?void 0:this.maxlength,minlength:x?void 0:this.minlength,readonly:this.readonly,tabindex:this.passivelyActivated&&!this.activated?-1:void 0,style:[this.textDecorationStyle[0],this.inputProps?.style,c],onBlur:this.handleInputBlur,onFocus:P=>{this.handleInputFocus(P,2)},onInput:this.handleInput,onChange:this.handleChange,onScroll:this.handleTextAreaScroll}),null,16,pt),this.showPlaceholder1?(s(),b("div",{class:v(`${e}-input__placeholder`),style:Q([this.placeholderStyle,c]),key:"placeholder"},[p(()=>this.mergedPlaceholder[0])],6)):p(()=>null),this.autosize?(s(),S(Do,{key:2,onResize:this.handleTextAreaMirrorResize},{default:()=>(s(),b("div",{ref:"textareaMirrorElRef",class:v(`${e}-input__textarea-mirror`),key:"mirror"},null,2))},1032,["onResize"])):p(()=>null)],64)}},1032,["class","container","theme","themeOverrides"])):(s(),b("div",{key:1,class:v(`${e}-input__input`)},[m("input",Pe({type:h==="password"&&this.mergedShowPasswordOn&&this.passwordVisible?"text":h},this.inputProps,{ref:"inputElRef",class:[`${e}-input__input-el`,this.inputProps?.class],style:[this.textDecorationStyle[0],this.inputProps?.style],tabindex:this.passivelyActivated&&!this.activated?-1:this.inputProps?.tabindex,placeholder:this.mergedPlaceholder[0],disabled:this.mergedDisabled,maxlength:x?void 0:this.maxlength,minlength:x?void 0:this.minlength,value:Array.isArray(this.mergedValue)?this.mergedValue[0]:this.mergedValue,readonly:this.readonly,autofocus:this.autofocus,size:this.attrSize,onBlur:this.handleInputBlur,onFocus:t=>{this.handleInputFocus(t,0)},onInput:t=>{this.handleInput(t,0)},onChange:t=>{this.handleChange(t,0)}}),null,16,vt),this.showPlaceholder1?(s(),b("div",{key:0,class:v(`${e}-input__placeholder`)},[m("span",null,[p(()=>this.mergedPlaceholder[0])])],2)):p(()=>null),this.autosize?(s(),b("div",{class:v(`${e}-input__input-mirror`),key:"mirror",ref:"inputMirrorElRef"}," ",2)):p(()=>null)],2)),p(()=>!this.pair&&ee(u.suffix,t=>t||this.clearable||this.showCount||this.mergedShowPasswordOn||this.loading!==void 0?(s(),b("div",{key:1,class:v(`${e}-input__suffix`)},[p(()=>[ee(u["clear-icon-placeholder"],c=>(this.clearable||c)&&(s(),S(ge,{clsPrefix:e,show:this.showClearButton,onClear:this.handleClear},{placeholder:()=>c,icon:()=>this.$slots["clear-icon"]?.()},1032,["clsPrefix","show","onClear"]))),this.internalLoadingBeforeSuffix?null:t,this.loading!==void 0?(s(),S(ct,{key:2,clsPrefix:e,loading:this.loading,showArrow:!1,showClear:!1,style:Q(this.cssVars)},null,8,["clsPrefix","loading","style"])):null,this.internalLoadingBeforeSuffix?t:null,this.showCount&&this.type!=="textarea"?(s(),S(Ee,{key:3},{default:c=>{const{renderCount:P}=this;return P?P(c):u.count?.(c)}},1024)):null,this.mergedShowPasswordOn&&this.type==="password"?(s(),b("div",{key:4,class:v(`${e}-input__eye`),onMousedown:this.handlePasswordToggleMousedown,onClick:this.handlePasswordToggleClick},[this.passwordVisible?(s(),b(fe,{key:0},[p(()=>G(u["password-visible-icon"],()=>[(s(),S(te,{clsPrefix:e},{default:()=>(s(),S(tt))},1032,["clsPrefix"]))]))],64)):(s(),b(fe,{key:1},[p(()=>G(u["password-invisible-icon"],()=>[(s(),S(te,{clsPrefix:e},{default:()=>(s(),S(nt))},1032,["clsPrefix"]))]))],64))],42,gt)):null])],2)):null))],2),this.pair?(s(),b("span",{key:0,class:v(`${e}-input__separator`)},[p(()=>G(u.separator,()=>[this.separator]))],2)):p(()=>null),this.pair?(s(),b("div",{key:2,class:v(`${e}-input-wrapper`)},[m("div",{class:v(`${e}-input__input`)},[m("input",{ref:"inputEl2Ref",type:this.type,class:v(`${e}-input__input-el`),tabindex:this.passivelyActivated&&!this.activated?-1:void 0,placeholder:this.mergedPlaceholder[1],disabled:this.mergedDisabled,maxlength:x?void 0:this.maxlength,minlength:x?void 0:this.minlength,value:Array.isArray(this.mergedValue)?this.mergedValue[1]:void 0,readonly:this.readonly,style:Q(this.textDecorationStyle[1]),onBlur:this.handleInputBlur,onFocus:t=>{this.handleInputFocus(t,1)},onInput:t=>{this.handleInput(t,1)},onChange:t=>{this.handleChange(t,1)}},null,46,bt),this.showPlaceholder2?(s(),b("div",{key:0,class:v(`${e}-input__placeholder`)},[m("span",null,[p(()=>this.mergedPlaceholder[1])])],2)):p(()=>null)],2),p(()=>ee(u.suffix,t=>(this.clearable||t)&&(s(),b("div",{class:v(`${e}-input__suffix`)},[p(()=>[this.clearable&&(s(),S(ge,{clsPrefix:e,show:this.showClearButton,onClear:this.handleClear},{icon:()=>u["clear-icon"]?.(),placeholder:()=>u["clear-icon-placeholder"]?.()},1032,["clsPrefix","show","onClear"])),t])],2))))],2)):p(()=>null),this.mergedBordered?(s(),b("div",{key:4,class:v(`${e}-input__border`)},null,2)):p(()=>null),this.mergedBordered?(s(),b("div",{key:6,class:v(`${e}-input__state-border`)},null,2)):p(()=>null),this.showCount&&h==="textarea"?(s(),S(Ee,{key:8},{default:t=>{const{renderCount:c}=this;return c?c(t):u.count?.(t)}},1024)):p(()=>null)],46,mt)}});export{st as C,zt as I,ct as S,rt as b,Qo as c,Ct as g,Xo as i,et as t,ot as u};
