import{aU as k,bI as r,d as T,aV as u,o as s,c as a,aX as n,F as R,ac as h,bs as m,aO as S,aY as V,bO as P,g,gT as _,a_ as w}from"./index-DpTHrIiy.js";import{u as D}from"./use-compitable-Cyx0ynjX.js";var F=k("text",`
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
 `)]);const O={...u.props,code:Boolean,type:{type:String,default:"default"},delete:Boolean,strong:Boolean,italic:Boolean,underline:Boolean,depth:[String,Number],tag:String,as:{type:String,validator:()=>!0,default:void 0}};var K=T({name:"Text",props:O,setup(e){const{mergedClsPrefixRef:o,inlineThemeDisabled:t}=V(e),f=u("Typography","-text",F,_,e,o),l=g(()=>{const{depth:d,type:c}=e,x=c==="default"?d===void 0?"textColor":`textColor${d}Depth`:w("textColor",c),{common:{fontWeightStrong:b,fontFamilyMono:y,cubicBezierEaseInOut:C},self:{codeTextColor:p,codeBorderRadius:v,codeColor:z,codeBorder:B,[x]:$}}=f.value;return{"--n-bezier":C,"--n-text-color":$,"--n-font-weight-strong":b,"--n-font-famliy-mono":y,"--n-code-border-radius":v,"--n-code-text-color":p,"--n-code-color":z,"--n-code-border":B}}),i=t?P("text",g(()=>`${e.type[0]}${e.depth||""}`),l,e):void 0;return{mergedClsPrefix:o,compitableTag:D(e,["as","tag"]),cssVars:t?void 0:l,themeClass:i?.themeClass,onRender:i?.onRender}},render(){const{mergedClsPrefix:e}=this;this.onRender?.();const o=[`${e}-text`,this.themeClass,{[`${e}-text--code`]:this.code,[`${e}-text--delete`]:this.delete,[`${e}-text--strong`]:this.strong,[`${e}-text--italic`]:this.italic,[`${e}-text--underline`]:this.underline}],t=this.$slots.default?.();return this.code?(s(),a("code",{key:1,class:m(o),style:h(this.cssVars)},[this.delete?(s(),a("del",{key:0},[n(()=>t)])):(s(),a(R,{key:1},[n(()=>t)],64))],6)):this.delete?(s(),a("del",{key:2,class:m(o),style:h(this.cssVars)},[n(()=>t)],6)):S(this.compitableTag||"span",{class:o,style:this.cssVars},t)}});export{K as t};
