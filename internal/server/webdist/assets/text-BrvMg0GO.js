import{y as l,I as T,$ as r,d as k,J as f,o as n,c as s,M as a,F as R,T as u,U as m,K as S,N as V,a1 as P,cA as w,P as D}from"./index-B-vQN22Y.js";function F(e,t){return l(()=>{for(const o of t)if(e[o]!==void 0)return e[o];return e[t[t.length-1]]})}var K=T("text",`
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
 `)]);const M={...f.props,code:Boolean,type:{type:String,default:"default"},delete:Boolean,strong:Boolean,italic:Boolean,underline:Boolean,depth:[String,Number],tag:String,as:{type:String,validator:()=>!0,default:void 0}};var _=k({name:"Text",props:M,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:o}=V(e),g=f("Typography","-text",K,w,e,t),i=l(()=>{const{depth:c,type:h}=e,x=h==="default"?c===void 0?"textColor":`textColor${c}Depth`:D("textColor",h),{common:{fontWeightStrong:y,fontFamilyMono:b,cubicBezierEaseInOut:C},self:{codeTextColor:p,codeBorderRadius:v,codeColor:$,codeBorder:z,[x]:B}}=g.value;return{"--n-bezier":C,"--n-text-color":B,"--n-font-weight-strong":y,"--n-font-famliy-mono":b,"--n-code-border-radius":v,"--n-code-text-color":p,"--n-code-color":$,"--n-code-border":z}}),d=o?P("text",l(()=>`${e.type[0]}${e.depth||""}`),i,e):void 0;return{mergedClsPrefix:t,compitableTag:F(e,["as","tag"]),cssVars:o?void 0:i,themeClass:d?.themeClass,onRender:d?.onRender}},render(){const{mergedClsPrefix:e}=this;this.onRender?.();const t=[`${e}-text`,this.themeClass,{[`${e}-text--code`]:this.code,[`${e}-text--delete`]:this.delete,[`${e}-text--strong`]:this.strong,[`${e}-text--italic`]:this.italic,[`${e}-text--underline`]:this.underline}],o=this.$slots.default?.();return this.code?(n(),s("code",{key:1,class:m(t),style:u(this.cssVars)},[this.delete?(n(),s("del",{key:0},[a(()=>o)])):(n(),s(R,{key:1},[a(()=>o)],64))],6)):this.delete?(n(),s("del",{key:2,class:m(t),style:u(this.cssVars)},[a(()=>o)],6)):S(this.compitableTag||"span",{class:t,style:this.cssVars},o)}});export{_ as t,F as u};
