<<<<<<<< HEAD:internal/server/webdist/assets/text-DepKJQdv.js
import{b8 as k,bY as r,d as T,b9 as b,o as s,c as n,bb as a,F as R,X as h,bI as m,b0 as S,bc as V,c2 as P,f as u,hZ as w,be as D}from"./index-BXADM5hg.js";import{u as F}from"./use-compitable-D5IlLREL.js";var _=k("text",`
========
import{b6 as k,bW as r,d as T,b7 as f,o as s,c as n,b9 as a,F as R,_ as h,bG as m,a_ as S,ba as V,c0 as _,f as u,ho as P,bc as w}from"./index-CnffFKTW.js";import{u as D}from"./use-compitable-DZG_Mast.js";var F=k("text",`
>>>>>>>> 1c67755a5 (fix(ui): unify editor and terminal window chrome and strengthen shadows):internal/server/webdist/assets/text-Bqdi1Wt-.js
 transition: color .3s var(--n-bezier);
 color: var(--n-text-color);
`,[r("strong",`
 font-weight: var(--n-font-weight-strong);
 `),r("italic",{fontStyle:"italic"}),r("underline",{textDecoration:"underline"}),r("code",`
 line-height: 1.4;
 display: inline-block;
 font-family: var(--n-font-famliy-mono);
 transition: 
 color .3s var(--n-bezier),
 border-color .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 box-sizing: border-box;
 padding: .05em .35em 0 .35em;
 border-radius: var(--n-code-border-radius);
 font-size: .9em;
 color: var(--n-code-text-color);
 background-color: var(--n-code-color);
 border: var(--n-code-border);
<<<<<<<< HEAD:internal/server/webdist/assets/text-DepKJQdv.js
 `)]);const E={...b.props,code:Boolean,type:{type:String,default:"default"},delete:Boolean,strong:Boolean,italic:Boolean,underline:Boolean,depth:[String,Number],tag:String,as:{type:String,validator:()=>!0,default:void 0}};var M=T({name:"Text",props:E,setup(e){const{mergedClsPrefixRef:o,inlineThemeDisabled:t}=V(e),f=b("Typography","-text",_,w,e,o),l=u(()=>{const{depth:d,type:c}=e,g=c==="default"?d===void 0?"textColor":`textColor${d}Depth`:D("textColor",c),{common:{fontWeightStrong:x,fontFamilyMono:y,cubicBezierEaseInOut:C},self:{codeTextColor:p,codeBorderRadius:v,codeColor:z,codeBorder:B,[g]:$}}=f.value;return{"--n-bezier":C,"--n-text-color":$,"--n-font-weight-strong":x,"--n-font-famliy-mono":y,"--n-code-border-radius":v,"--n-code-text-color":p,"--n-code-color":z,"--n-code-border":B}}),i=t?P("text",u(()=>`${e.type[0]}${e.depth||""}`),l,e):void 0;return{mergedClsPrefix:o,compitableTag:F(e,["as","tag"]),cssVars:t?void 0:l,themeClass:i?.themeClass,onRender:i?.onRender}},render(){const{mergedClsPrefix:e}=this;this.onRender?.();const o=[`${e}-text`,this.themeClass,{[`${e}-text--code`]:this.code,[`${e}-text--delete`]:this.delete,[`${e}-text--strong`]:this.strong,[`${e}-text--italic`]:this.italic,[`${e}-text--underline`]:this.underline}],t=this.$slots.default?.();return this.code?(s(),n("code",{key:1,class:m(o),style:h(this.cssVars)},[this.delete?(s(),n("del",{key:0},[a(()=>t)])):(s(),n(R,{key:1},[a(()=>t)],64))],6)):this.delete?(s(),n("del",{key:2,class:m(o),style:h(this.cssVars)},[a(()=>t)],6)):S(this.compitableTag||"span",{class:o,style:this.cssVars},t)}});export{M as t};
========
 `)]);const E={...f.props,code:Boolean,type:{type:String,default:"default"},delete:Boolean,strong:Boolean,italic:Boolean,underline:Boolean,depth:[String,Number],tag:String,as:{type:String,validator:()=>!0,default:void 0}};var N=T({name:"Text",props:E,setup(e){const{mergedClsPrefixRef:o,inlineThemeDisabled:t}=V(e),g=f("Typography","-text",F,P,e,o),l=u(()=>{const{depth:d,type:c}=e,b=c==="default"?d===void 0?"textColor":`textColor${d}Depth`:w("textColor",c),{common:{fontWeightStrong:x,fontFamilyMono:y,cubicBezierEaseInOut:C},self:{codeTextColor:p,codeBorderRadius:v,codeColor:z,codeBorder:B,[b]:$}}=g.value;return{"--n-bezier":C,"--n-text-color":$,"--n-font-weight-strong":x,"--n-font-famliy-mono":y,"--n-code-border-radius":v,"--n-code-text-color":p,"--n-code-color":z,"--n-code-border":B}}),i=t?_("text",u(()=>`${e.type[0]}${e.depth||""}`),l,e):void 0;return{mergedClsPrefix:o,compitableTag:D(e,["as","tag"]),cssVars:t?void 0:l,themeClass:i?.themeClass,onRender:i?.onRender}},render(){const{mergedClsPrefix:e}=this;this.onRender?.();const o=[`${e}-text`,this.themeClass,{[`${e}-text--code`]:this.code,[`${e}-text--delete`]:this.delete,[`${e}-text--strong`]:this.strong,[`${e}-text--italic`]:this.italic,[`${e}-text--underline`]:this.underline}],t=this.$slots.default?.();return this.code?(s(),n("code",{key:1,class:m(o),style:h(this.cssVars)},[this.delete?(s(),n("del",{key:0},[a(()=>t)])):(s(),n(R,{key:1},[a(()=>t)],64))],6)):this.delete?(s(),n("del",{key:2,class:m(o),style:h(this.cssVars)},[a(()=>t)],6)):S(this.compitableTag||"span",{class:o,style:this.cssVars},t)}});export{N as t};
>>>>>>>> 1c67755a5 (fix(ui): unify editor and terminal window chrome and strengthen shadows):internal/server/webdist/assets/text-Bqdi1Wt-.js
