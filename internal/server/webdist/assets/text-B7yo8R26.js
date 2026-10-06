<<<<<<<< HEAD:internal/server/webdist/assets/text-B6mTGXEZ.js
import{Z as k,an as r,d as T,_ as u,o as s,c as n,a1 as a,F as R,af as h,ag as m,$ as S,a2 as V,ap as P,g,eA as _,a4 as w}from"./index-x7ocYgEb.js";import{u as D}from"./use-compitable-L3mQCklQ.js";var F=k("text",`
========
import{W as k,ak as r,d as T,X as g,o as s,c as n,_ as a,F as R,ac as m,ad as h,Y as S,$ as V,am as P,z as u,et as _,a1 as w}from"./index-By3w5_xg.js";import{u as D}from"./use-compitable-CeE-C9ts.js";var F=k("text",`
>>>>>>>> 153bd781 (feat(i18n): localize domains, teams and notification channels en/vi (I18N-8)):internal/server/webdist/assets/text-B7yo8R26.js
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
 `)]);const E={...u.props,code:Boolean,type:{type:String,default:"default"},delete:Boolean,strong:Boolean,italic:Boolean,underline:Boolean,depth:[String,Number],tag:String,as:{type:String,validator:()=>!0,default:void 0}};var N=T({name:"Text",props:E,setup(e){const{mergedClsPrefixRef:o,inlineThemeDisabled:t}=V(e),f=u("Typography","-text",F,_,e,o),l=g(()=>{const{depth:d,type:c}=e,x=c==="default"?d===void 0?"textColor":`textColor${d}Depth`:w("textColor",c),{common:{fontWeightStrong:y,fontFamilyMono:b,cubicBezierEaseInOut:p},self:{codeTextColor:C,codeBorderRadius:v,codeColor:$,codeBorder:z,[x]:B}}=f.value;return{"--n-bezier":p,"--n-text-color":B,"--n-font-weight-strong":y,"--n-font-famliy-mono":b,"--n-code-border-radius":v,"--n-code-text-color":C,"--n-code-color":$,"--n-code-border":z}}),i=t?P("text",g(()=>`${e.type[0]}${e.depth||""}`),l,e):void 0;return{mergedClsPrefix:o,compitableTag:D(e,["as","tag"]),cssVars:t?void 0:l,themeClass:i?.themeClass,onRender:i?.onRender}},render(){const{mergedClsPrefix:e}=this;this.onRender?.();const o=[`${e}-text`,this.themeClass,{[`${e}-text--code`]:this.code,[`${e}-text--delete`]:this.delete,[`${e}-text--strong`]:this.strong,[`${e}-text--italic`]:this.italic,[`${e}-text--underline`]:this.underline}],t=this.$slots.default?.();return this.code?(s(),n("code",{key:1,class:m(o),style:h(this.cssVars)},[this.delete?(s(),n("del",{key:0},[a(()=>t)])):(s(),n(R,{key:1},[a(()=>t)],64))],6)):this.delete?(s(),n("del",{key:2,class:m(o),style:h(this.cssVars)},[a(()=>t)],6)):S(this.compitableTag||"span",{class:o,style:this.cssVars},t)}});export{N as t};
